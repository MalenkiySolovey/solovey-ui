package hostsurface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type RawProcess struct {
	PID              int
	ParentPID        int
	SessionID        int
	StartTime        string
	Executable       string
	ExeDevice        uint64
	ExeInode         uint64
	UID              int
	GID              int
	ControlGroup     string
	EvidenceRevision string
	ProviderRevision string
}

type RawSocket struct {
	Network   hostfacts.Network
	Family    hostfacts.Family
	Bind      string
	Port      uint16
	Protocol  string
	Inode     string
	Processes []RawProcess
}

type PlatformSnapshot struct {
	Sockets     []RawSocket
	Truncated   bool
	ReasonCodes []string
}

type Provider struct {
	Now             func() time.Time
	Resources       func(context.Context) hostresources.ResourceSnapshot
	ObservePlatform func(context.Context, hostfacts.Limits) (PlatformSnapshot, error)
	OwnerObserver   OwnerObserver
	SSHProjection   SSHProjectionSource
	BindingRevision string
}

var providerGeneration atomic.Uint64

const (
	ownerObservationTimeout = 80 * time.Second
	maxOwnerWorkers         = 4
)

func NewProvider(ownerObservers ...OwnerObserver) *Provider {
	provider := &Provider{
		Now:             time.Now,
		Resources:       func(ctx context.Context) hostresources.ResourceSnapshot { return hostresources.Snapshot(ctx) },
		ObservePlatform: observePlatform,
		BindingRevision: hostresources.Revision(struct {
			Source     string
			Generation uint64
		}{"server-protection:linux-hostsurface", providerGeneration.Add(1)}),
	}
	if len(ownerObservers) > 0 {
		provider.OwnerObserver = ownerObservers[0]
	}
	return provider
}

func NewProviderWithSSHProjection(source SSHProjectionSource, ownerObservers ...OwnerObserver) *Provider {
	provider := NewProvider(ownerObservers...)
	provider.SSHProjection = source
	provider.Resources = func(ctx context.Context) hostresources.ResourceSnapshot {
		return hostresources.SnapshotExcluding(ctx, domain.ProtectionResourceOwner)
	}
	return provider
}

func (*Provider) SourceID() string { return "server-protection:linux-hostsurface" }

// ObservationTimeout is consumed by the neutral registry through its bounded
// provider-budget interface. It covers one platform scan plus concurrent,
// individually bounded 60-second listener-owner observations.
func (*Provider) ObservationTimeout() time.Duration { return ownerObservationTimeout }

func (p *Provider) Observe(ctx context.Context, limits hostfacts.Limits) (hostfacts.Observation, error) {
	if p == nil {
		return hostfacts.Observation{}, errors.New("hostsurface provider is nil")
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	resources := hostresources.ResourceSnapshot{}
	if p.Resources != nil {
		resources = p.Resources(ctx)
	}
	ownerStates := make(map[string]ownerObservationState, len(resources.Resources)*2)
	ownerResources := append([]hostresources.ProtectableResource(nil), resources.Resources...)
	sort.Slice(ownerResources, func(i, j int) bool { return ownerResources[i].ID < ownerResources[j].ID })
	type ownerTask struct {
		key      string
		resource hostresources.ProtectableResource
	}
	type ownerResult struct {
		key         string
		observation OwnerObservation
	}
	tasks := make([]ownerTask, 0, len(ownerResources)*2)
	for _, resource := range ownerResources {
		intents, intentsValid := hostresources.ConfiguredListenIntents(resource)
		if !intentsValid {
			ownerStates[ownerStateKey(resource.ID, hostresources.NetworkUnknown)] = ownerState(unavailableOwnerObservation(OwnerObservationFailed, "listener_owner_listen_intent_invalid"), p.BindingRevision)
			continue
		}
		_, expectedValid := listenerOwnerExpectationFor(resource)
		for _, intent := range intents {
			key := ownerStateKey(resource.ID, intent.Network)
			if !expectedValid {
				ownerStates[key] = ownerState(unavailableOwnerObservation(OwnerContractMismatch, "listener_owner_expectation_missing"), p.BindingRevision)
				continue
			}
			if p.OwnerObserver == nil || !exactOwnerRevision(p.BindingRevision) {
				ownerStates[key] = ownerState(unavailableOwnerObservation(OwnerObserverNotBound), p.BindingRevision)
				continue
			}
			copy := resource
			copy.ListenIntent = intent
			copy.ListenIntents = nil
			tasks = append(tasks, ownerTask{key: key, resource: copy})
		}
	}
	if len(tasks) > 0 {
		jobs, results := make(chan ownerTask), make(chan ownerResult, len(tasks))
		workers := min(maxOwnerWorkers, len(tasks))
		var wait sync.WaitGroup
		wait.Add(workers)
		for range workers {
			go func() {
				defer wait.Done()
				for task := range jobs {
					results <- ownerResult{task.key, p.OwnerObserver.ObserveOwner(ctx, task.resource)}
				}
			}()
		}
		for _, task := range tasks {
			jobs <- task
		}
		close(jobs)
		wait.Wait()
		close(results)
		for result := range results {
			ownerStates[result.key] = ownerState(result.observation, p.BindingRevision)
		}
		ownerStates = fenceOwnerInvocationSet(ownerStates)
	}
	// Acquire the SSH owner generation and the kernel socket surface adjacent
	// to final normalization. Long-running non-SSH owner observations therefore
	// cannot consume a short-lived SSH authority before it is projected.
	sshProjection := SSHListenerProjection{}
	sshProjectionReason := ""
	if p.SSHProjection != nil {
		var projectionErr error
		sshProjection, projectionErr = p.SSHProjection.ProjectSSHListeners(ctx, now)
		if projectionErr != nil {
			sshProjectionReason = "ssh_owner_projection_unavailable"
		} else {
			resources.Resources = append(resources.Resources, sshProjection.Resources...)
			sort.Slice(resources.Resources, func(i, j int) bool { return resources.Resources[i].ID < resources.Resources[j].ID })
		}
	}
	observe := p.ObservePlatform
	if observe == nil {
		observe = observePlatform
	}
	raw, err := observe(ctx, limits)
	if err != nil {
		return hostfacts.Observation{}, err
	}
	validationTime := sshProjection.ValidatedAt
	if validationTime.IsZero() {
		validationTime = now().UTC()
	}
	return NormalizeWithOwnersAndSSH(raw, resources, ownerStates, sshProjection, sshProjectionReason, validationTime), nil
}

type ownerObservationState struct {
	Available                     bool
	Availability                  OwnerAvailability
	Observation                   *protectionhelper.ListenerOwnerObserveResult
	HelperIdentityRevision        string
	CapabilityRevision            string
	ListenerOwnerContractRevision string
	ListenerOwnerObserverRevision string
	ProviderBindingRevision       string
	ReasonCodes                   []string
}

func ownerStateKey(resourceID string, network hostresources.Network) string {
	return resourceID + "|" + string(network)
}

func ownerState(observation OwnerObservation, providerBinding string) ownerObservationState {
	return ownerObservationState{
		Available: observation.Availability == OwnerObservationSuccess, Availability: observation.Availability,
		Observation: observation.Observation, HelperIdentityRevision: observation.HelperIdentityRevision,
		CapabilityRevision: observation.CapabilityRevision, ListenerOwnerContractRevision: observation.ListenerOwnerContractRevision,
		ListenerOwnerObserverRevision: observation.ListenerOwnerObserverRevision, ProviderBindingRevision: providerBinding,
		ReasonCodes: append([]string(nil), observation.ReasonCodes...),
	}
}

func fenceOwnerInvocationSet(states map[string]ownerObservationState) map[string]ownerObservationState {
	var helperIdentity, capabilityRevision string
	identityMismatch, capabilityMismatch := false, false
	for _, state := range states {
		if state.HelperIdentityRevision != "" {
			if helperIdentity == "" {
				helperIdentity = state.HelperIdentityRevision
			} else if helperIdentity != state.HelperIdentityRevision {
				identityMismatch = true
			}
		}
		if state.CapabilityRevision != "" {
			if capabilityRevision == "" {
				capabilityRevision = state.CapabilityRevision
			} else if capabilityRevision != state.CapabilityRevision {
				capabilityMismatch = true
			}
		}
	}
	if !identityMismatch && !capabilityMismatch {
		return states
	}
	for resourceID, state := range states {
		availability := OwnerHelperCapabilityStale
		if identityMismatch {
			availability = OwnerHelperIdentityMismatch
		}
		failure := ownerObservationFailure(OwnerObservation{
			HelperIdentityRevision: state.HelperIdentityRevision, CapabilityRevision: state.CapabilityRevision,
			ListenerOwnerContractRevision: state.ListenerOwnerContractRevision, ListenerOwnerObserverRevision: state.ListenerOwnerObserverRevision,
		}, availability)
		states[resourceID] = ownerState(failure, state.ProviderBindingRevision)
	}
	return states
}

func Normalize(raw PlatformSnapshot, resources hostresources.ResourceSnapshot, now time.Time) hostfacts.Observation {
	return NormalizeWithOwners(raw, resources, nil, now)
}

func NormalizeWithOwners(raw PlatformSnapshot, resources hostresources.ResourceSnapshot, owners map[string]ownerObservationState, now time.Time) hostfacts.Observation {
	return NormalizeWithOwnersAndSSH(raw, resources, owners, SSHListenerProjection{}, "", now)
}

func NormalizeWithOwnersAndSSH(raw PlatformSnapshot, resources hostresources.ResourceSnapshot, owners map[string]ownerObservationState, ssh SSHListenerProjection, sshReason string, now time.Time) hostfacts.Observation {
	result := hostfacts.Observation{Facts: make([]hostfacts.HostSurfaceFactV1, 0, len(raw.Sockets)), Truncated: raw.Truncated, ReasonCodes: append([]string(nil), raw.ReasonCodes...)}
	if sshReason != "" {
		result.ReasonCodes = append(result.ReasonCodes, sshReason)
	}
	matchedResources := make(map[string]bool)
	for _, socket := range raw.Sockets {
		fact := hostfacts.HostSurfaceFactV1{
			Schema: hostfacts.SchemaV1, Network: socket.Network, Family: socket.Family,
			Bind: canonicalBind(socket.Bind), Port: socket.Port, Protocol: strings.ToLower(strings.TrimSpace(socket.Protocol)),
			Exposure: exposureForBind(socket.Bind), SocketInode: safeNumeric(socket.Inode),
			OwnershipMode: hostfacts.OwnershipUnmanaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: now.Add(90 * time.Second).Unix(),
			Source: "server-protection:proc-inet-diag", ConfigurationRevision: hostresources.Revision(struct {
				Network, Family, Bind string
				Port                  uint16
			}{string(socket.Network), string(socket.Family), canonicalBind(socket.Bind), socket.Port}),
		}
		processes := socket.Processes
		processEvidenceComplete := false
		if len(processes) == 1 {
			process := processes[0]
			pid, parent, session, uid, gid := process.PID, process.ParentPID, process.SessionID, process.UID, process.GID
			fact.Process = hostfacts.ProcessFact{ProviderRevision: process.ProviderRevision, EvidenceRevision: process.EvidenceRevision,
				PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: process.StartTime,
				Executable: process.Executable, ExeDevice: process.ExeDevice, ExeInode: process.ExeInode,
				UID: &uid, GID: &gid, ControlGroup: process.ControlGroup}
			processEvidenceComplete = completeProcessEvidence(process)
			if processEvidenceComplete {
				fact.Process.ExeDigest = digestToken(process.Executable + "|" + process.EvidenceRevision)
				fact.ConfidenceBP = 9000
			} else {
				fact.ReasonCodes = append(fact.ReasonCodes, "process_evidence_incomplete")
			}
		} else if len(processes) > 1 {
			fact.ReasonCodes = append(fact.ReasonCodes, "process_owner_ambiguous")
		} else {
			fact.ReasonCodes = append(fact.ReasonCodes, "process_owner_unknown")
		}
		sshMatched := false
		if authority, resourceID, ok := matchSSHProjection(socket, ssh, now); ok {
			fact.SemanticOwner = &hostfacts.SemanticOwnerFactV1{
				ManagementService: hostfacts.ManagementServiceSSH, Source: ssh.Source, Revision: ssh.Revision,
				AuthorityRevision: authority.Revision, Socket: authority.Socket,
			}
			fact.ConfigurationRevision = authority.ConfigurationRevision
			fact.SocketInode, fact.SocketCookie = authority.Socket.Inode, authority.Socket.Cookie
			fact.Process, fact.Service = authority.Process, authority.Service
			fact.RegisteredResourceID, fact.DesiredOwner, fact.OwnershipMode = resourceID, domain.ProtectionResourceOwner, hostfacts.OwnershipExternalManaged
			fact.Classification, fact.ConfidenceBP = hostfacts.ClassificationExpectedExternal, 10000
			fact.Source = ssh.Source
			fact.FirstSeen, fact.LastSeen, fact.ExpiresAt = authority.ObservedAt, authority.ObservedAt, authority.ExpiresAt
			fact.ReasonCodes = withoutReasons(fact.ReasonCodes, "process_owner_unknown", "process_owner_ambiguous", "process_evidence_incomplete", "process_owner_not_verified")
			fact.ReasonCodes = append(fact.ReasonCodes, "ssh_listener_authority_exact")
			matchedResources[ownerStateKey(resourceID, hostresources.Network(socket.Network))] = true
			sshMatched = true
		}
		if sshMatched {
			// Exact SSH ownership and its resource binding come from the SSH
			// semantic authority. A same-socket configured resource remains a
			// graph collision and cannot overwrite this classification.
		} else if resource := matchResource(socket, resources.Resources); resource != nil {
			key := ownerStateKey(resource.ID, hostresources.Network(socket.Network))
			matchedResources[key] = true
			fact.RegisteredResourceID = resource.ID
			fact.DesiredOwner = resource.Owner
			fact.ConfigurationRevision = resource.Capabilities.ConfigRevision
			state := owners[key]
			ownerFacts := matchingOwnerFacts(socket, state.Observation, *resource, now)
			switch {
			case len(ownerFacts) == 1:
				owner := ownerFacts[0]
				fact.SocketInode, fact.SocketCookie = owner.Socket.Inode, owner.Socket.Cookie
				fact.Process, fact.Service, fact.ListenerOwner = owner.Process, owner.Service, &owner
				fact.OwnershipMode, fact.Classification, fact.ConfidenceBP = hostfacts.OwnershipManaged, hostfacts.ClassificationManagedExact, 10000
				fact.ReasonCodes = append(fact.ReasonCodes, "listener_owner_exact")
			case len(ownerFacts) > 1:
				fact.OwnershipMode, fact.Classification, fact.ConfidenceBP = hostfacts.OwnershipUnmanaged, hostfacts.ClassificationUnknownOwner, 0
				fact.ReasonCodes = append(fact.ReasonCodes, "listener_owner_ambiguous")
			case ownerStateHas(state, "listener_owner_stale") || ownerStateHas(state, "listener_deployment_mismatch"):
				fact.OwnershipMode, fact.Classification, fact.ConfidenceBP = hostfacts.OwnershipUnmanaged, hostfacts.ClassificationStale, 0
				fact.ReasonCodes = append(fact.ReasonCodes, state.ReasonCodes...)
			case ownerStateUnavailable(state):
				fact.OwnershipMode, fact.Classification, fact.ConfidenceBP = hostfacts.OwnershipUnmanaged, hostfacts.ClassificationUnknownOwner, 0
				fact.ReasonCodes = append(fact.ReasonCodes, state.ReasonCodes...)
			case ownerStateHas(state, "listener_owner_ambiguous") || ownerStateHas(state, "listener_owner_fact_invalid") || ownerStateHas(state, "listener_owner_revision_missing"):
				fact.OwnershipMode, fact.Classification, fact.ConfidenceBP = hostfacts.OwnershipUnmanaged, hostfacts.ClassificationUnknownOwner, 0
				fact.ReasonCodes = append(fact.ReasonCodes, state.ReasonCodes...)
			case state.Available && processEvidenceComplete:
				fact.OwnershipMode, fact.Classification, fact.ConfidenceBP = hostfacts.OwnershipUnmanaged, hostfacts.ClassificationForeign, 0
				fact.ReasonCodes = append(fact.ReasonCodes, "listener_owner_foreign")
			default:
				fact.OwnershipMode, fact.Classification = hostfacts.OwnershipUnmanaged, hostfacts.ClassificationUnknownOwner
				fact.ReasonCodes = append(fact.ReasonCodes, state.ReasonCodes...)
				fact.ReasonCodes = append(fact.ReasonCodes, "process_owner_not_verified")
				if processEvidenceComplete && (fact.ConfidenceBP > 5000 || fact.ConfidenceBP == 0) {
					fact.ConfidenceBP = 5000
				}
			}
		} else if len(socket.Processes) == 1 && !processEvidenceComplete {
			fact.Classification = hostfacts.ClassificationUnknownOwner
		} else if fact.Exposure == hostfacts.ExposureLocal || fact.Exposure == hostfacts.ExposurePrivate {
			fact.Classification = hostfacts.ClassificationLocalOnly
		} else if len(socket.Processes) != 1 {
			fact.Classification = hostfacts.ClassificationUnknownOwner
		} else {
			fact.Classification = hostfacts.ClassificationUnexpectedPublic
		}
		fact.ID = hostfacts.StableID(fact)
		result.Facts = append(result.Facts, fact)
	}
	for _, resource := range resources.Resources {
		intents, valid := hostresources.ConfiguredListenIntents(resource)
		if !valid {
			continue
		}
		for _, intent := range intents {
			key := ownerStateKey(resource.ID, intent.Network)
			if matchedResources[key] {
				continue
			}
			state := owners[key]
			family := hostfacts.FamilyUnknown
			if len(intent.RequiredFamilies) == 1 {
				family = hostfacts.Family(intent.RequiredFamilies[0])
			}
			fact := hostfacts.HostSurfaceFactV1{
				Schema: hostfacts.SchemaV1, Network: hostfacts.Network(intent.Network), Family: family,
				Bind: intent.Address, Port: intent.Port, Protocol: string(intent.Network),
				Exposure: exposureForBind(intent.Address), RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner,
				OwnershipMode: hostfacts.OwnershipUnmanaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
				Source: "server-protection:listener-owner", ConfidenceBP: 0, ConfigurationRevision: resource.Capabilities.ConfigRevision,
				Classification: hostfacts.ClassificationUnobserved, ReasonCodes: append([]string{"listener_unobserved"}, state.ReasonCodes...),
			}
			fact.ID = hostfacts.StableID(fact)
			result.Facts = append(result.Facts, fact)
		}
	}
	sort.Slice(result.Facts, func(i, j int) bool { return result.Facts[i].ID < result.Facts[j].ID })
	ownerRevisionInputs := append([]string(nil), result.ReasonCodes...)
	for resourceID, state := range owners {
		ownerRevisionInputs = append(ownerRevisionInputs, resourceID)
		ownerRevisionInputs = append(ownerRevisionInputs, string(state.Availability), state.HelperIdentityRevision, state.CapabilityRevision,
			state.ListenerOwnerContractRevision, state.ListenerOwnerObserverRevision, state.ProviderBindingRevision)
		if state.Observation != nil {
			ownerRevisionInputs = append(ownerRevisionInputs, state.Observation.ObservationRevision)
		}
		ownerRevisionInputs = append(ownerRevisionInputs, state.ReasonCodes...)
	}
	result.OwnerObservationRevision = hostfacts.OwnerObservationSetRevision(result.Facts, ownerRevisionInputs)
	return result
}

func matchSSHProjection(socket RawSocket, projection SSHListenerProjection, now time.Time) (domain.SSHListenerAuthorityV1, string, bool) {
	if projection.Source == "" || !exactOwnerRevision(projection.Revision) || len(projection.Authorities) == 0 || len(projection.Authorities) > 64 || len(projection.ResourceIDs) != len(projection.Authorities) {
		return domain.SSHListenerAuthorityV1{}, "", false
	}
	type match struct {
		authority  domain.SSHListenerAuthorityV1
		resourceID string
	}
	matches := make([]match, 0, 1)
	for _, authority := range projection.Authorities {
		if !authority.Valid(now) || authority.Socket.Network != socket.Network || authority.Socket.Family != socket.Family || authority.Socket.Port != socket.Port ||
			authority.Socket.Bind != canonicalBind(socket.Bind) || authority.Socket.Inode != safeNumeric(socket.Inode) {
			continue
		}
		resourceID := projection.ResourceIDs[authority.Revision]
		if resourceID == "" || resourceID != domain.ProtectionResourceID(authority) {
			continue
		}
		matches = append(matches, match{authority: authority, resourceID: resourceID})
	}
	if len(matches) != 1 {
		return domain.SSHListenerAuthorityV1{}, "", false
	}
	return matches[0].authority, matches[0].resourceID, true
}

func withoutReasons(values []string, rejected ...string) []string {
	blocked := make(map[string]bool, len(rejected))
	for _, value := range rejected {
		blocked[value] = true
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !blocked[value] {
			result = append(result, value)
		}
	}
	return result
}

func matchingOwnerFacts(socket RawSocket, observation *protectionhelper.ListenerOwnerObserveResult, resource hostresources.ProtectableResource, now time.Time) []hostfacts.ListenerOwnerFactV1 {
	if observation == nil {
		return nil
	}
	expected, expectedValid := listenerOwnerExpectationFor(resource)
	if !expectedValid {
		return nil
	}
	result := make([]hostfacts.ListenerOwnerFactV1, 0, 1)
	for _, fact := range observation.Facts {
		application := fact.Application
		if fact.Valid(now) && application.ResourceID == resource.ID &&
			application.ResourceOwnerRevision == resource.Capabilities.OwnerRevision && application.ConfigurationRevision == resource.Capabilities.ConfigRevision &&
			application.OwnerContractRevision == expected.ContractRevision && application.InstanceID == expected.InstanceID &&
			application.SourceRevision == expected.SourceRevision && application.ArtifactRevision == expected.ArtifactRevision &&
			application.DeploymentID == expected.DeploymentID && application.RuntimeRootBindingRevision == expected.RuntimeRootBindingRevision &&
			application.ExpectedExecutableSHA256 == expected.ExecutableSHA256 && application.ServiceIdentity == expected.ServiceIdentity &&
			fact.Process.Executable == expected.ExecutablePath &&
			fact.Process.UID != nil && fact.Process.GID != nil && uint32(*fact.Process.UID) == expected.ProcessUID && uint32(*fact.Process.GID) == expected.ProcessGID &&
			fact.Socket.Network == socket.Network && fact.Socket.Family == socket.Family &&
			fact.Socket.Bind == canonicalBind(socket.Bind) && fact.Socket.Port == socket.Port && fact.Socket.Inode == socket.Inode {
			result = append(result, fact)
		}
	}
	return result
}

func ownerStateHas(state ownerObservationState, reason string) bool {
	for _, value := range state.ReasonCodes {
		if value == reason {
			return true
		}
	}
	return false
}

func ownerStateUnavailable(state ownerObservationState) bool {
	for _, reason := range []string{"listener_owner_capability_unavailable", "listener_owner_contract_unavailable", "listener_owner_systemd_unavailable", "listener_owner_procd_unavailable", "listener_owner_unavailable", "listener_owner_scan_bounded", "listener_service_unavailable"} {
		if ownerStateHas(state, reason) {
			return true
		}
	}
	return false
}

func matchResource(socket RawSocket, resources []hostresources.ProtectableResource) *hostresources.ProtectableResource {
	for index := range resources {
		if !resources[index].Capabilities.Known {
			continue
		}
		intents, valid := hostresources.ConfiguredListenIntents(resources[index])
		if !valid {
			continue
		}
		for _, intent := range intents {
			if string(intent.Network) == string(socket.Network) && intent.Port == socket.Port && socketMatchesIntent(socket, intent) {
				return &resources[index]
			}
		}
	}
	return nil
}

func socketMatchesIntent(socket RawSocket, intent hostresources.ConfiguredListenIntentV1) bool {
	observed := hostresources.NormalizeListen(socket.Bind)
	switch intent.Mode {
	case hostresources.ListenIntentExact:
		return len(intent.RequiredFamilies) == 1 && hostresources.AddressFamily(socket.Family) == intent.RequiredFamilies[0] && observed.Value == intent.Address
	case hostresources.ListenIntentWildcard:
		switch hostresources.NormalizeListen(intent.Address).Class {
		case hostresources.ListenWildcard:
			return observed.Wildcard() && (socket.Family == hostfacts.FamilyIPv4 || socket.Family == hostfacts.FamilyIPv6)
		case hostresources.ListenIPv4Wildcard:
			return socket.Family == hostfacts.FamilyIPv4 && observed.Value == "0.0.0.0"
		case hostresources.ListenIPv6Wildcard:
			return socket.Family == hostfacts.FamilyIPv6 && observed.Value == "::"
		}
	case hostresources.ListenIntentDualStack:
		return intent.Address == "*" && observed.Wildcard() && (socket.Family == hostfacts.FamilyIPv4 || socket.Family == hostfacts.FamilyIPv6)
	}
	return false
}

func exposureForBind(value string) hostfacts.Exposure {
	normalized := hostresources.NormalizeListen(value)
	if normalized.Class == hostresources.ListenLoopback {
		return hostfacts.ExposureLocal
	}
	if addr, err := netip.ParseAddr(normalized.Value); err == nil && addr.IsPrivate() {
		return hostfacts.ExposurePrivate
	}
	if normalized.Class == hostresources.ListenHostname || normalized.Class == hostresources.ListenWildcard {
		return hostfacts.ExposureUnknown
	}
	return hostfacts.ExposurePublic
}

func canonicalBind(value string) string { return hostresources.NormalizeListen(value).Value }

func digestToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func completeProcessEvidence(value RawProcess) bool {
	return value.PID > 0 && value.ParentPID >= 0 && value.SessionID >= 0 && safeNumeric(value.StartTime) != "" &&
		canonicalProcessExecutable(value.Executable) && value.ExeDevice != 0 && value.ExeInode != 0 && value.UID >= 0 && value.GID >= 0 &&
		safeProcessProviderRevision(value.ProviderRevision) && exactOwnerRevision(value.EvidenceRevision)
}

func canonicalProcessExecutable(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) > 1 && len(value) <= 512 && path.IsAbs(value) && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}

func safeProcessProviderRevision(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n\t")
}

func safeNumeric(value string) string {
	for _, r := range value {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return value
}
