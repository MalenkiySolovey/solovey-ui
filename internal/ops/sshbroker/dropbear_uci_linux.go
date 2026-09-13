//go:build linux

package sshbroker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"golang.org/x/sys/unix"
)

const (
	dropbearConfigName      = "dropbear"
	dropbearManagedMarker   = "SoloveyPolicyDigest"
	dropbearCheckpointV1    = 1
	maxDropbearSections     = 32
	maxDropbearOptions      = 128
	maxDropbearOptionValues = 64
	maxDropbearLogRecords   = 256
)

var (
	dropbearEndpointPattern = regexp.MustCompile(`^management:ssh:configured:(ipv4|ipv6):([0-9]{1,5})(:([A-Za-z0-9_-]{1,64}):([a-f0-9]{16}))?$`)
	dropbearOptionName      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
)

type dropbearUCIHost struct {
	dropbear          *executableobject.Object
	uci               *executableobject.Object
	service           sshServiceControlAdapter
	rollbackAuthority broker.CompletedMutationAuthority
}

type uciOption struct {
	List   bool     `json:"list"`
	Values []string `json:"values"`
}

type uciSection struct {
	Name      string               `json:"name,omitempty"`
	Anonymous bool                 `json:"anonymous"`
	Internal  string               `json:"internal"`
	TypeIndex int                  `json:"typeIndex"`
	Type      string               `json:"type"`
	Options   map[string]uciOption `json:"options"`
}

type dropbearConfig struct {
	Sections []uciSection `json:"sections"`
	Revision string       `json:"revision"`
}

type dropbearCheckpoint struct {
	Schema                  int        `json:"schema"`
	OperationID             string     `json:"operationId"`
	EndpointID              string     `json:"endpointId"`
	CandidateArtifactDigest string     `json:"candidateArtifactDigest"`
	PriorArtifactDigest     string     `json:"priorArtifactDigest"`
	Configuration           string     `json:"configurationRevision"`
	SectionFingerprint      string     `json:"sectionFingerprint"`
	Section                 uciSection `json:"section"`
}

type dropbearInstanceEvidence struct {
	Name     string
	PID      int
	Command  []string
	Start    string
	Revision string
}

func newDropbearUCIHost(service sshServiceControlAdapter) (*dropbearUCIHost, error) {
	dropbear, err := firstFixedBinary("/usr/sbin/dropbear")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errDropbearBinaryUnavailable, err)
	}
	uci, err := firstFixedBinary("/sbin/uci")
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, errors.New("Dropbear service-control adapter is unavailable")
	}
	return &dropbearUCIHost{dropbear: dropbear, uci: uci, service: service}, nil
}

func (h *dropbearUCIHost) observe(ctx context.Context, now time.Time) (ObservationV1, error) {
	config, err := h.readConfig(ctx)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "configuration_evidence_unavailable", "configuration_read", "", "")
	}
	if err := h.dropbear.Revalidate(); err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "binary_identity_unavailable", "process_fence", "", "")
	}
	binaryIdentity := h.dropbear.Identity()
	binaryContentSHA256 := binaryIdentity.Digest
	if binaryContentSHA256 == "" {
		return ObservationV1{}, listenerAuthorityWrap(errors.New("selected Dropbear binary identity is unavailable"), "binary_identity_unavailable", "process_fence", "", "")
	}
	version, _ := h.run(ctx, h.dropbear, nil, "-V")
	serviceObservation, err := h.service.Observe(ctx)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "service_evidence_unavailable", "service_observe", "", "")
	}
	serviceRaw := serviceObservation.Evidence
	serviceRevision := domain.Revision(struct {
		Evidence, Binary string
	}{domain.Revision(serviceRaw), binaryContentSHA256})
	graph := make([]domain.ConfigNodeV1, 0, len(config.Sections)+1)
	graph = append(graph, domain.ConfigNodeV1{ID: "uci:dropbear", Kind: "uci_package", Order: 0, Depth: 0,
		Digest: config.Revision, Owner: "root", ModeClass: "owner_read_write"})
	contexts := make([]domain.MatchContextV1, 0, len(config.Sections))
	endpoints := make([]hostresources.ManagementEndpointV1, 0, len(config.Sections)*2)
	authorities := make([]domain.SSHListenerAuthorityV1, 0, len(config.Sections)*2)
	instances := make([]dropbearInstanceEvidence, 0, len(config.Sections))
	enabled := make([]uciSection, 0, len(config.Sections))
	for _, section := range config.Sections {
		if section.Type != dropbearConfigName {
			continue
		}
		fingerprint := dropbearSectionFingerprint(section)
		id := "uci:" + section.Internal
		graph = append(graph, domain.ConfigNodeV1{ID: id, ParentID: "uci:dropbear", Kind: "uci_section", Order: uint16(len(graph)), Depth: 1,
			Digest: fingerprint, Owner: "root", ModeClass: "owner_read_write"})
		contexts = append(contexts, domain.MatchContextV1{ID: id, ConditionClass: "dropbear_instance", EffectiveHash: fingerprint, Known: true})
		enabledSection, err := dropbearSectionEnabled(section)
		if err != nil {
			return ObservationV1{}, listenerAuthorityWrap(err, "configuration_evidence_invalid", "authority_validate", "", "")
		}
		if !enabledSection {
			continue
		}
		enabled = append(enabled, section)
		port, err := dropbearSectionPort(section)
		if err != nil {
			return ObservationV1{}, listenerAuthorityWrap(err, "configuration_evidence_invalid", "authority_validate", "", "")
		}
		instance, instanceEndpoints, instanceAuthorities, err := h.observeDropbearInstance(ctx, serviceRaw, section, port, binaryIdentity, serviceRevision, config.Revision, now)
		if err != nil {
			return ObservationV1{}, err
		}
		instances = append(instances, instance)
		endpoints = append(endpoints, instanceEndpoints...)
		authorities = append(authorities, instanceAuthorities...)
	}
	if len(enabled) == 0 || len(endpoints) == 0 || len(authorities) == 0 {
		return ObservationV1{}, listenerAuthorityFailure("management_instance_unavailable", "configured_listener_match", "", "")
	}
	afterService, err := h.service.Observe(ctx)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "service_generation_changed", "stability_recheck", "", "")
	}
	for index, section := range enabled {
		port, _ := dropbearSectionPort(section)
		after, err := parseDropbearProcdInstance(afterService.Evidence, section.Internal, port, h.dropbear.Label())
		if err != nil || !sameDropbearInstance(instances[index], after) {
			if err == nil {
				err = errors.New("Dropbear procd instance changed during listener observation")
			}
			return ObservationV1{}, listenerAuthorityWrap(err, "instance_generation_changed", "stability_recheck", "", "")
		}
		fresh, err := processevidence.Observe(after.PID)
		if err != nil || fresh.StartTime != instances[index].Start || !sameSSHExecutableObject(fresh, binaryIdentity) {
			if err == nil {
				err = errors.New("Dropbear process changed during listener observation")
			}
			return ObservationV1{}, listenerAuthorityWrap(err, "process_generation_changed", "stability_recheck", "", "")
		}
	}
	authentication, forwarding, err := projectDropbearRuntimePosture(enabled, instances)
	if err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "runtime_posture_invalid", "authority_validate", "", "")
	}
	capabilities := domain.CapabilitySetV1{ObservePosture: domain.AvailabilityAvailable, Prepare: domain.AvailabilityAvailable,
		Stage: domain.AvailabilityAvailable, Validate: domain.AvailabilityAvailable, Reload: domain.AvailabilityAvailable,
		Reconnect: domain.AvailabilityAvailable, Rollback: domain.AvailabilityAvailable}
	capabilities.Revision = domain.Revision(capabilities)
	posture := domain.SSHPostureV1{Schema: domain.PostureSchemaV1,
		Binary:      domain.BinaryIdentityV1{Implementation: "dropbear", VersionClass: dropbearVersionClass(string(version)), Digest: binaryContentSHA256, Selected: true},
		Service:     domain.ServiceIdentityV1{Manager: string(h.service.Kind()), UnitID: serviceObservation.ID, State: "active", Digest: serviceRevision},
		ConfigGraph: graph, MatchContexts: contexts, Endpoints: endpoints, ListenerAuthorities: authorities, Authentication: authentication,
		Forwarding: forwarding, AuthorizedKeys: dropbearAuthorizedKeys(), HostKeys: dropbearHostKeys(),
		Capabilities: capabilities, ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxPostureLifetime).Unix(),
		BinaryRevision: binaryContentSHA256, ServiceRevision: serviceRevision, ConfigurationRevision: config.Revision}
	posture.SemanticRevision = domain.PostureSemanticRevision(posture)
	if err := posture.Validate(now); err != nil {
		return ObservationV1{}, listenerAuthorityWrap(err, "posture_invalid", "authority_validate", "", "")
	}
	return ObservationV1{Posture: posture, ProviderRevision: ProviderRevision}, nil
}

func (h *dropbearUCIHost) observeDropbearInstance(ctx context.Context, serviceRaw []byte, section uciSection, port uint16, binaryIdentity executableobject.Identity, serviceRevision, configurationRevision string, now time.Time) (dropbearInstanceEvidence, []hostresources.ManagementEndpointV1, []domain.SSHListenerAuthorityV1, error) {
	binaryContentSHA256 := binaryIdentity.Digest
	instance, err := parseDropbearProcdInstance(serviceRaw, section.Internal, port, h.dropbear.Label())
	if err != nil {
		return dropbearInstanceEvidence{}, nil, nil, listenerAuthorityWrap(err, "instance_evidence_unavailable", "process_fence", "", "")
	}
	process, err := observeSSHProcess(ctx, instance.PID, binaryIdentity)
	if err != nil {
		return dropbearInstanceEvidence{}, nil, nil, listenerAuthorityWrap(err, "process_identity_unavailable", "process_fence", "", "")
	}
	instance.Start = process.StartTime
	instance.Revision = domain.Revision(struct {
		Name, Start string
		PID         int
		Command     []string
	}{instance.Name, instance.Start, instance.PID, instance.Command})
	observation, err := listenerevidence.ObserveAcceptingTCPDetailed(ctx, instance.PID, map[uint16]bool{port: true})
	if err != nil {
		return dropbearInstanceEvidence{}, nil, nil, fmt.Errorf("Dropbear accepting listener authority is unavailable: %w", err)
	}
	sockets := observation.Sockets
	if len(sockets) == 0 {
		return dropbearInstanceEvidence{}, nil, nil, listenerAuthorityFailure("socket_not_accepting", "configured_listener_match", observation.Diagnostic.ErrnoClass, observation.Diagnostic.ProofMethod)
	}
	if !dropbearCommandExactlyCoversSockets(instance.Command, sockets) {
		return dropbearInstanceEvidence{}, nil, nil, listenerAuthorityFailure("socket_identity_mismatch", "procd_command_match", observation.Diagnostic.ErrnoClass, observation.Diagnostic.ProofMethod)
	}
	cgroupAvailability := "unavailable"
	if process.ControlGroup != "" {
		cgroupAvailability = "available"
	}
	pid := instance.PID
	service := hostfacts.ServiceFact{
		SupervisorRevision: domain.Revision(struct {
			Kind, Service, Instance, Process string
		}{"procd", dropbearConfigName, instance.Name, process.EvidenceRevision}),
		CgroupAvailability: cgroupAvailability, CgroupPolicy: "optional",
		CgroupRevision: domain.Revision(struct {
			Provider, Availability, Policy, Path, Service, Instance string
		}{processevidence.RevisionV2, cgroupAvailability, "optional", process.ControlGroup, dropbearConfigName, instance.Name}),
		ControlGroup: process.ControlGroup, MainPID: &pid, ActiveState: "active", SubState: "running",
		ProcdService: dropbearConfigName, ProcdInstance: instance.Name, ProcdCommand: append([]string(nil), instance.Command...),
	}
	endpoints, authorities, err := projectDropbearListenerAuthorities(sockets, section, instance, process, service, binaryContentSHA256, serviceRevision, configurationRevision, now)
	if err != nil {
		return dropbearInstanceEvidence{}, nil, nil, listenerAuthorityWrap(err, "socket_identity_invalid", "inode_correlate", "", observation.Diagnostic.ProofMethod)
	}
	return instance, endpoints, authorities, nil
}

// projectDropbearListenerAuthorities is the semantic ambiguity boundary after
// low-level socket observation. The observer must retain distinct kernel
// sockets; Dropbear must reject two sockets that would claim one canonical
// configured endpoint instead of choosing an inode arbitrarily.
func projectDropbearListenerAuthorities(sockets []hostfacts.ListenerSocketIdentityV1, section uciSection, instance dropbearInstanceEvidence, process hostfacts.ProcessFact, service hostfacts.ServiceFact, binaryContentSHA256, serviceRevision, configurationRevision string, now time.Time) ([]hostresources.ManagementEndpointV1, []domain.SSHListenerAuthorityV1, error) {
	endpoints := make([]hostresources.ManagementEndpointV1, 0, len(sockets)*2)
	authorities := make([]domain.SSHListenerAuthorityV1, 0, len(sockets))
	seenIDs := map[string]bool{}
	for _, socket := range sockets {
		endpointIDs := make([]string, 0, len(socket.CoverageFamilies))
		for _, covered := range socket.CoverageFamilies {
			family := hostresources.AddressFamily(covered)
			bind, ok := dropbearEndpointBind(socket, family)
			if !ok {
				return nil, nil, errors.New("Dropbear listener family coverage is malformed")
			}
			id := dropbearEndpointID(section, family, bind, socket.Port)
			if seenIDs[id] {
				return nil, nil, errors.New("Dropbear listener endpoint identity is ambiguous")
			}
			seenIDs[id] = true
			endpointIDs = append(endpointIDs, id)
			endpoint := hostresources.ManagementEndpointV1{
				Schema: hostresources.ManagementEndpointSchemaV1, ID: id, Network: hostresources.NetworkTCP, Family: family,
				Bind: bind, Port: socket.Port, ServiceKind: hostresources.ManagementSSH,
				Exposure: hostresources.EndpointIntentForBind(bind), Owner: "system", Purpose: "ssh_administrative_access",
				RecoveryPolicy: "fresh_independent_path_required", Source: "privileged_broker", ConfiguredIntent: true,
				Wildcard: socket.Wildcard, DualStack: len(socket.CoverageFamilies) == 2, ConfidenceBP: 10000,
				ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxPostureLifetime).Unix(), ConfigurationRevision: configurationRevision,
			}
			endpoints = append(endpoints, endpoint)
		}
		if len(endpointIDs) == 0 {
			return nil, nil, errors.New("Dropbear listener has no address-family coverage")
		}
		authority := domain.SSHListenerAuthorityV1{
			Schema: domain.ListenerAuthoritySchemaV1, EndpointIDs: endpointIDs, InstanceID: instance.Name,
			Socket: socket, Process: process, Service: service, BinaryRevision: binaryContentSHA256,
			ServiceRevision: serviceRevision, ConfigurationRevision: configurationRevision,
			ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxListenerAuthorityLifetime).Unix(),
		}
		authority.Seal()
		authorities = append(authorities, authority)
	}
	return endpoints, authorities, nil
}

func dropbearEndpointBind(socket hostfacts.ListenerSocketIdentityV1, family hostresources.AddressFamily) (string, bool) {
	if !socket.Wildcard {
		return socket.Bind, family == hostresources.AddressFamily(socket.Family)
	}
	switch family {
	case hostresources.AddressFamilyIPv4:
		return "0.0.0.0", true
	case hostresources.AddressFamilyIPv6:
		return "::", true
	default:
		return "", false
	}
}

func observeSSHProcess(ctx context.Context, pid int, binary executableobject.Identity) (hostfacts.ProcessFact, error) {
	evidence, err := processevidence.Observe(pid)
	if err != nil || !sameSSHExecutableObject(evidence, binary) || evidence.UID != 0 || evidence.GID != 0 ||
		evidence.ExeMode&unix.S_IFMT != unix.S_IFREG || evidence.ExeMode&0o111 == 0 || evidence.ExeMode&0o022 != 0 ||
		evidence.ExeUID != 0 || evidence.ExeGID != 0 || evidence.ExeDevice == 0 || evidence.ExeInode == 0 {
		return hostfacts.ProcessFact{}, errors.New("Dropbear process executable identity is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return hostfacts.ProcessFact{}, err
	}
	cgroup := ""
	switch evidence.CgroupAvailability {
	case processevidence.CgroupAvailable:
		observed, available := processevidence.UnifiedCgroup(evidence)
		if !available {
			return hostfacts.ProcessFact{}, errors.New("Dropbear process cgroup evidence is malformed")
		}
		cgroup = observed
	case processevidence.CgroupUnavailable:
	default:
		return hostfacts.ProcessFact{}, errors.New("Dropbear process cgroup evidence is unsafe")
	}
	return hostfacts.ProcessFact{
		ProviderRevision: evidence.ProviderRevision, EvidenceRevision: evidence.Revision,
		PID: &pid, ParentPID: &evidence.ParentPID, SessionID: &evidence.SessionID, StartTime: evidence.StartTime,
		ExeDigest: evidence.ExeDigest, Executable: evidence.Executable, ExeDevice: evidence.ExeDevice, ExeInode: evidence.ExeInode,
		UID: &evidence.UID, GID: &evidence.GID, ControlGroup: cgroup,
	}, nil
}

// sameSSHExecutableObject compares two observations in the raw executable
// content/object domain owned by executableobject and processevidence. A JSON
// revision of either structure is deliberately not accepted as content
// identity. Device and inode keep pathname replacement fail-closed even when
// replacement bytes happen to be identical.
func sameSSHExecutableObject(process processevidence.Fact, binary executableobject.Identity) bool {
	return !process.ExecutableDeleted && binary.Label != "" && binary.Digest != "" &&
		process.Executable == binary.Label && process.ExeDigest == binary.Digest &&
		process.ExeDevice == binary.Device && process.ExeInode == binary.Inode && process.ExeSize == binary.Size &&
		process.ExeMode == uint32(binary.Mode) && process.ExeUID == binary.UID && process.ExeGID == binary.GID
}

func sameDropbearInstance(left, right dropbearInstanceEvidence) bool {
	return left.Name == right.Name && left.PID == right.PID && domain.Revision(left.Command) == domain.Revision(right.Command)
}

func dropbearEndpointID(section uciSection, family hostresources.AddressFamily, bind string, port uint16) string {
	return fmt.Sprintf("management:ssh:configured:%s:%d:%s:%s", family, port, section.Internal, domain.Revision(bind)[:16])
}

func (h *dropbearUCIHost) stage(ctx context.Context, envelope broker.Request, request StageRequestV1) (any, error) {
	before, err := h.observe(ctx, time.Now().UTC().Truncate(time.Second))
	if err != nil || checkExpected(envelope, before.Posture) != nil {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear host revision changed before staging")
	}
	config, err := h.readConfig(ctx)
	if err != nil || config.Revision != before.Posture.ConfigurationRevision {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear UCI revision changed before staging")
	}
	section, err := selectDropbearSection(config, request.EndpointID)
	if err != nil {
		return nil, broker.Failure(broker.CodeValidation, "Dropbear target instance is absent or ambiguous")
	}
	prepared, policy, err := prepareDropbearUCIPolicy(request.Policy)
	if err != nil || prepared.ArtifactDigest != request.ExpectedArtifactDigest {
		return nil, broker.Failure(broker.CodeValidation, "SSH policy is not representable by Dropbear")
	}
	candidateDigest := prepared.ArtifactDigest
	priorDigest := dropbearArtifactDigest(section)
	checkpoint := dropbearCheckpoint{Schema: dropbearCheckpointV1, OperationID: envelope.OperationID, EndpointID: request.EndpointID,
		CandidateArtifactDigest: candidateDigest, PriorArtifactDigest: priorDigest, Configuration: config.Revision,
		SectionFingerprint: dropbearSectionFingerprint(section), Section: cloneUCISection(section)}
	checkpointBytes, _ := json.Marshal(checkpoint)
	if len(checkpointBytes) == 0 || len(checkpointBytes) > MaxCheckpointBytes {
		return nil, broker.Failure(broker.CodeValidation, "Dropbear rollback checkpoint exceeds its bound")
	}
	fresh, err := h.readConfig(ctx)
	if err != nil || fresh.Revision != config.Revision {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear UCI revision changed during target selection")
	}
	freshSection, err := selectDropbearSection(fresh, request.EndpointID)
	if err != nil || dropbearSectionFingerprint(freshSection) != checkpoint.SectionFingerprint {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear target instance changed during selection")
	}
	expected := cloneUCISection(freshSection)
	applyDropbearPolicy(&expected, policy, candidateDigest)
	if err := h.replaceSectionOptions(ctx, freshSection, expected.Options); err != nil {
		partial, readErr := h.readConfig(ctx)
		if partialSection, selectErr := selectDropbearSection(partial, request.EndpointID); readErr == nil && selectErr == nil && dropbearArtifactDigest(partialSection) == candidateDigest {
			return StageResultV1{ArtifactDigest: candidateDigest, EndpointID: request.EndpointID, Prior: dropbearPrior(checkpointBytes, priorDigest), ProviderRevision: ProviderRevision, ConfigurationRevision: partial.Revision},
				broker.Failure(broker.CodeRecoveryRequired, "Dropbear staged state requires recovery")
		}
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear UCI transaction outcome is not safely attributable")
	}
	after, err := h.readConfig(ctx)
	if err != nil || !dropbearUnrelatedSectionsEqual(fresh, after, freshSection.Internal) {
		return StageResultV1{ArtifactDigest: candidateDigest, EndpointID: request.EndpointID, Prior: dropbearPrior(checkpointBytes, priorDigest), ProviderRevision: ProviderRevision, ConfigurationRevision: after.Revision},
			broker.Failure(broker.CodeRecoveryRequired, "Dropbear UCI transaction changed unrelated state")
	}
	staged, err := selectDropbearSection(after, request.EndpointID)
	if err != nil || !dropbearOptionsEqual(staged.Options, expected.Options) || dropbearArtifactDigest(staged) != candidateDigest {
		return StageResultV1{ArtifactDigest: candidateDigest, EndpointID: request.EndpointID, Prior: dropbearPrior(checkpointBytes, priorDigest), ProviderRevision: ProviderRevision, ConfigurationRevision: after.Revision},
			broker.Failure(broker.CodeRecoveryRequired, "Dropbear staged state could not be verified")
	}
	return StageResultV1{ArtifactDigest: candidateDigest, EndpointID: request.EndpointID, Prior: dropbearPrior(checkpointBytes, priorDigest),
		ProviderRevision: ProviderRevision, ConfigurationRevision: after.Revision}, nil
}

func (h *dropbearUCIHost) validate(ctx context.Context, envelope broker.Request, request ValidationRequestV1) (any, error) {
	observation, config, section, err := h.currentTarget(ctx, envelope, request.EndpointID, request.ArtifactDigest)
	if err != nil {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear staged target changed before validation")
	}
	// The pinned OpenWrt Dropbear init script has no validate action. The
	// validated proposition here is Solovey's typed, freshly re-read UCI target;
	// OpenWrt applies the native service definition during the reload step and
	// the following process/endpoint correlation verifies that result.
	return ValidationResultV1{SyntaxValid: true, EffectiveValid: true,
		EffectiveRevision: domain.Revision(struct{ Configuration, Section string }{config.Revision, dropbearSectionFingerprint(section)}),
		ProviderRevision:  observation.ProviderRevision}, nil
}

func (h *dropbearUCIHost) reload(ctx context.Context, envelope broker.Request, request ReloadRequestV1) (any, error) {
	_, before, selected, err := h.currentTarget(ctx, envelope, request.EndpointID, request.ArtifactDigest)
	if err != nil {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear staged target changed before reload")
	}
	if err := h.service.Reload(ctx); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "Dropbear reload failed")
	}
	after, err := h.readConfig(ctx)
	if err != nil || after.Revision != before.Revision {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear configuration changed during reload")
	}
	selectedAfter, err := selectDropbearSection(after, request.EndpointID)
	if err != nil || dropbearArtifactDigest(selectedAfter) != request.ArtifactDigest || dropbearSectionFingerprint(selectedAfter) != dropbearSectionFingerprint(selected) {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear effective target differs after reload")
	}
	instance, err := h.selectedInstance(ctx, selectedAfter)
	if err != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear procd instance cannot be correlated after reload")
	}
	postReload, err := h.observe(ctx, time.Now().UTC().Truncate(time.Second))
	if err != nil || postReload.Posture.ConfigurationRevision != after.Revision || instance.Revision == "" {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear post-reload posture cannot be verified")
	}
	return ReloadResultV1{ServiceRevision: postReload.Posture.ServiceRevision, ConfigurationRevision: after.Revision, ProviderRevision: ProviderRevision}, nil
}

func (h *dropbearUCIHost) restore(ctx context.Context, envelope broker.Request, request RestoreRequestV1) (any, error) {
	checkpoint, prior, err := h.authoritativeCheckpoint(envelope, request)
	if err != nil {
		return nil, err
	}
	_, current, selected, err := h.currentTarget(ctx, envelope, request.EndpointID, request.ExpectedCurrentArtifactDigest)
	if err != nil {
		return nil, broker.Failure(broker.CodeFence, "Dropbear rollback would overwrite foreign state")
	}
	if !sameDropbearSectionIdentity(selected, checkpoint.Section) {
		return nil, broker.Failure(broker.CodeFence, "Dropbear rollback target identity changed")
	}
	if err := h.replaceSectionOptions(ctx, selected, checkpoint.Section.Options); err != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear rollback restore failed")
	}
	after, err := h.readConfig(ctx)
	if err != nil || !dropbearUnrelatedSectionsEqual(current, after, selected.Internal) {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear rollback changed unrelated state")
	}
	restored, err := selectDropbearSection(after, request.EndpointID)
	if err != nil || !dropbearOptionsEqual(restored.Options, checkpoint.Section.Options) || dropbearArtifactDigest(restored) != prior.Digest {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "Dropbear rollback exact-state verification failed")
	}
	return RestoreResultV1{ArtifactDigest: prior.Digest, ConfigurationRevision: after.Revision, ProviderRevision: ProviderRevision}, nil
}

func (h *dropbearUCIHost) authoritativeCheckpoint(envelope broker.Request, request RestoreRequestV1) (dropbearCheckpoint, PriorArtifactV1, error) {
	staged, err := authoritativeDropbearStagePrior(h.rollbackAuthority, envelope, request)
	if err != nil {
		return dropbearCheckpoint{}, PriorArtifactV1{}, err
	}
	var checkpoint dropbearCheckpoint
	if broker.DecodePayload(staged.Prior.Content, &checkpoint) != nil || !validDropbearCheckpoint(checkpoint) ||
		checkpoint.OperationID != envelope.OperationID || checkpoint.EndpointID != request.EndpointID ||
		checkpoint.CandidateArtifactDigest != staged.ArtifactDigest || checkpoint.PriorArtifactDigest != staged.Prior.Digest {
		return dropbearCheckpoint{}, PriorArtifactV1{}, broker.Failure(broker.CodeRecoveryRequired, "Dropbear rollback authority is corrupt")
	}
	return checkpoint, staged.Prior, nil
}

func validDropbearCheckpoint(checkpoint dropbearCheckpoint) bool {
	if checkpoint.Schema != dropbearCheckpointV1 || !safeToken(checkpoint.OperationID, 64) || !safeToken(checkpoint.EndpointID, 256) ||
		!digest(checkpoint.CandidateArtifactDigest) || !digest(checkpoint.PriorArtifactDigest) || !digest(checkpoint.Configuration) ||
		checkpoint.SectionFingerprint != dropbearSectionFingerprint(checkpoint.Section) || checkpoint.Section.Type != dropbearConfigName ||
		dropbearSelector(checkpoint.Section) == "" || len(checkpoint.Section.Options) > maxDropbearOptions {
		return false
	}
	for name, option := range checkpoint.Section.Options {
		if !dropbearOptionName.MatchString(name) || len(option.Values) == 0 || len(option.Values) > maxDropbearOptionValues || !option.List && len(option.Values) != 1 {
			return false
		}
		for _, value := range option.Values {
			if len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
				return false
			}
		}
	}
	return true
}

func sameDropbearSectionIdentity(left, right uciSection) bool {
	return left.Name == right.Name && left.Anonymous == right.Anonymous && left.Internal == right.Internal &&
		left.TypeIndex == right.TypeIndex && left.Type == right.Type
}

func (h *dropbearUCIHost) inspect(ctx context.Context, envelope broker.Request, request InspectRequestV1) (any, error) {
	observation, err := h.observe(ctx, time.Now().UTC().Truncate(time.Second))
	if err != nil || checkExpected(envelope, observation.Posture) != nil {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear host revision changed before inspection")
	}
	config, err := h.readConfig(ctx)
	if err != nil || config.Revision != observation.Posture.ConfigurationRevision {
		return nil, broker.Failure(broker.CodeRevision, "Dropbear UCI revision changed before inspection")
	}
	section, err := selectDropbearSection(config, request.EndpointID)
	if err != nil {
		return nil, broker.Failure(broker.CodeValidation, "Dropbear target instance is absent or ambiguous")
	}
	return InspectResultV1{Present: true, ArtifactDigest: dropbearArtifactDigest(section), Owner: "root", Group: "root",
		ModeClass: "owner_read_write", Mode: 0o600, ConfigurationRevision: config.Revision}, nil
}

func (h *dropbearUCIHost) currentTarget(ctx context.Context, envelope broker.Request, endpointID, artifactDigest string) (ObservationV1, dropbearConfig, uciSection, error) {
	observation, err := h.observe(ctx, time.Now().UTC().Truncate(time.Second))
	if err != nil || checkExpected(envelope, observation.Posture) != nil {
		return ObservationV1{}, dropbearConfig{}, uciSection{}, errors.New("Dropbear posture changed")
	}
	config, err := h.readConfig(ctx)
	if err != nil || config.Revision != observation.Posture.ConfigurationRevision {
		return ObservationV1{}, dropbearConfig{}, uciSection{}, errors.New("Dropbear UCI revision changed")
	}
	section, err := selectDropbearSection(config, endpointID)
	if err != nil || dropbearArtifactDigest(section) != artifactDigest {
		return ObservationV1{}, dropbearConfig{}, uciSection{}, errors.New("Dropbear artifact changed")
	}
	return observation, config, section, nil
}

func (h *dropbearUCIHost) readConfig(ctx context.Context) (dropbearConfig, error) {
	exported, err := h.run(ctx, h.uci, nil, "-q", "export", dropbearConfigName)
	if err != nil || len(exported) == 0 {
		return dropbearConfig{}, errors.New("bounded Dropbear UCI export failed")
	}
	shown, err := h.run(ctx, h.uci, nil, "-q", "-X", "show", dropbearConfigName)
	if err != nil || len(shown) == 0 {
		return dropbearConfig{}, errors.New("bounded Dropbear UCI section projection failed")
	}
	sections, err := parseDropbearUCI(exported, shown)
	if err != nil {
		return dropbearConfig{}, err
	}
	config := dropbearConfig{Sections: sections}
	config.Revision = domain.Revision(config.Sections)
	return config, nil
}

func parseDropbearUCI(exported, shown []byte) ([]uciSection, error) {
	if len(exported) == 0 || len(exported) > maxCommandOutput || len(shown) == 0 || len(shown) > maxCommandOutput ||
		bytes.IndexByte(exported, 0) >= 0 || bytes.IndexByte(shown, 0) >= 0 {
		return nil, errors.New("Dropbear UCI evidence is malformed or oversized")
	}
	sections := make([]uciSection, 0)
	var current *uciSection
	scanner := bufio.NewScanner(bytes.NewReader(exported))
	scanner.Buffer(make([]byte, 64<<10), maxCommandOutput)
	packageSeen := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		tokens, err := parseUCILine(line)
		if err != nil || len(tokens) == 0 {
			return nil, errors.New("Dropbear UCI export contains malformed syntax")
		}
		switch tokens[0] {
		case "package":
			if packageSeen || len(tokens) != 2 || tokens[1] != dropbearConfigName {
				return nil, errors.New("Dropbear UCI package identity differs")
			}
			packageSeen = true
		case "config":
			if !packageSeen || len(tokens) < 2 || len(tokens) > 3 || len(sections) >= maxDropbearSections || tokens[1] != dropbearConfigName {
				return nil, errors.New("Dropbear UCI section is invalid")
			}
			section := uciSection{Type: tokens[1], TypeIndex: len(sections), Anonymous: len(tokens) == 2, Options: map[string]uciOption{}}
			if len(tokens) == 3 {
				section.Name = tokens[2]
			}
			sections = append(sections, section)
			current = &sections[len(sections)-1]
		case "option", "list":
			if current == nil || len(tokens) != 3 || !dropbearOptionName.MatchString(tokens[1]) || len(tokens[2]) > 4096 || strings.ContainsAny(tokens[2], "\x00\r\n") || len(current.Options) >= maxDropbearOptions {
				return nil, errors.New("Dropbear UCI option is invalid")
			}
			option := current.Options[tokens[1]]
			isList := tokens[0] == "list"
			if len(option.Values) > 0 && (option.List != isList || !isList) || len(option.Values) >= maxDropbearOptionValues {
				return nil, errors.New("Dropbear UCI option representation is ambiguous")
			}
			option.List = isList
			option.Values = append(option.Values, tokens[2])
			current.Options[tokens[1]] = option
		default:
			return nil, errors.New("Dropbear UCI export contains unsupported syntax")
		}
	}
	if scanner.Err() != nil || !packageSeen || len(sections) == 0 {
		return nil, errors.New("Dropbear UCI export is incomplete")
	}
	internal := make([]string, 0, len(sections))
	seenInternal := make(map[string]bool, len(sections))
	scanner = bufio.NewScanner(bytes.NewReader(shown))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		left, right, ok := strings.Cut(line, "=")
		if !ok || right != dropbearConfigName || !strings.HasPrefix(left, dropbearConfigName+".") {
			continue
		}
		ref := strings.TrimPrefix(left, dropbearConfigName+".")
		if strings.Contains(ref, ".") || !safeUCISectionRef(ref) {
			return nil, errors.New("Dropbear UCI internal section identity is malformed")
		}
		if seenInternal[ref] {
			return nil, errors.New("Dropbear UCI internal section identity is ambiguous")
		}
		seenInternal[ref] = true
		internal = append(internal, ref)
	}
	if scanner.Err() != nil || len(internal) != len(sections) {
		return nil, errors.New("Dropbear UCI section projections disagree")
	}
	for index := range sections {
		sections[index].Internal = internal[index]
		if !sections[index].Anonymous && sections[index].Name != internal[index] {
			return nil, errors.New("Dropbear named UCI section identity differs")
		}
	}
	return sections, nil
}

func parseUCILine(line string) ([]string, error) {
	result := make([]string, 0, 4)
	for index := 0; index < len(line); {
		for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index == len(line) {
			break
		}
		var value strings.Builder
		for index < len(line) && line[index] != ' ' && line[index] != '\t' {
			if line[index] != '\'' {
				value.WriteByte(line[index])
				index++
				continue
			}
			index++
			closed := false
			for index < len(line) {
				if line[index] == '\'' {
					index++
					closed = true
					break
				}
				value.WriteByte(line[index])
				index++
			}
			if !closed {
				return nil, errors.New("unterminated UCI quoted value")
			}
			if index+2 < len(line) && line[index] == '\\' && line[index+1] == '\'' && line[index+2] == '\'' {
				value.WriteByte('\'')
				index += 3
			}
		}
		result = append(result, value.String())
	}
	return result, nil
}

func selectDropbearSection(config dropbearConfig, endpointID string) (uciSection, error) {
	match := dropbearEndpointPattern.FindStringSubmatch(endpointID)
	if match == nil {
		return uciSection{}, errors.New("Dropbear endpoint identity is invalid")
	}
	port64, _ := strconv.ParseUint(match[2], 10, 16)
	if port64 == 0 {
		return uciSection{}, errors.New("Dropbear endpoint port is invalid")
	}
	matches := make([]uciSection, 0, 1)
	for _, section := range config.Sections {
		if section.Type != dropbearConfigName {
			continue
		}
		enabled, enabledErr := dropbearSectionEnabled(section)
		if enabledErr != nil {
			return uciSection{}, enabledErr
		}
		port, err := dropbearSectionPort(section)
		sectionMatch := match[4] == "" || section.Internal == match[4]
		if enabled && sectionMatch && err == nil && port == uint16(port64) {
			matches = append(matches, section)
		}
	}
	if len(matches) != 1 {
		return uciSection{}, errors.New("Dropbear endpoint does not select exactly one enabled section")
	}
	return matches[0], nil
}

func dropbearSectionEnabled(section uciSection) (bool, error) {
	return dropbearBoolOption(section, "enable", true)
}

func dropbearSectionPort(section uciSection) (uint16, error) {
	value, ok, err := strictScalarOption(section, "Port")
	if err != nil {
		return 0, errors.New("Dropbear section port is malformed")
	}
	if !ok {
		return 22, nil
	}
	parsed, err := parseDropbearUnsigned(value, 16)
	if err != nil || parsed == 0 {
		return 0, errors.New("Dropbear section port is invalid")
	}
	return uint16(parsed), nil
}

func parseUCIBool(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "1", "on", "true", "yes", "enabled":
		return true, true
	case "0", "off", "false", "no", "disabled":
		return false, true
	default:
		return false, false
	}
}

func parseDropbearUnsigned(value string, bits int) (uint64, error) {
	if value == "" {
		return 0, errors.New("empty unsigned integer")
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errors.New("non-decimal unsigned integer")
		}
	}
	return strconv.ParseUint(value, 10, bits)
}

func strictScalarOption(section uciSection, name string) (string, bool, error) {
	option, ok := section.Options[name]
	if !ok {
		return "", false, nil
	}
	if option.List || len(option.Values) != 1 {
		return "", false, errors.New("Dropbear scalar UCI option is ambiguous")
	}
	return option.Values[0], true, nil
}

func optionScalar(section uciSection, name string) (string, bool) {
	option, ok := section.Options[name]
	return func() (string, bool) {
		if !ok || option.List || len(option.Values) != 1 {
			return "", false
		}
		return option.Values[0], true
	}()
}

func applyDropbearPolicy(section *uciSection, policy dropbearUCIPolicy, marker string) {
	set := func(name, value string) { section.Options[name] = uciOption{Values: []string{value}} }
	if policy.MaxAuthTries != nil {
		set("MaxAuthTries", strconv.FormatUint(uint64(*policy.MaxAuthTries), 10))
	}
	if policy.PasswordAuth != nil {
		set("PasswordAuth", bool01(*policy.PasswordAuth))
	}
	if policy.RootLogin != nil {
		set("RootLogin", bool01(*policy.RootLogin))
	}
	if policy.RootPasswordAuth != nil {
		set("RootPasswordAuth", bool01(*policy.RootPasswordAuth))
	}
	set(dropbearManagedMarker, marker)
}

func (h *dropbearUCIHost) replaceSectionOptions(ctx context.Context, section uciSection, desired map[string]uciOption) error {
	selector := dropbearSelector(section)
	if selector == "" {
		return errors.New("Dropbear UCI selector is unavailable")
	}
	names := make([]string, 0, len(section.Options)+len(desired))
	seen := map[string]bool{}
	for name := range section.Options {
		seen[name] = true
		names = append(names, name)
	}
	for name := range desired {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var batch strings.Builder
	for _, name := range names {
		if !dropbearOptionName.MatchString(name) {
			return errors.New("Dropbear UCI option name is unsafe")
		}
		if _, exists := section.Options[name]; exists {
			fmt.Fprintf(&batch, "delete %s.%s.%s\n", dropbearConfigName, selector, name)
		}
		option, present := desired[name]
		if !present {
			continue
		}
		for index, value := range option.Values {
			if strings.ContainsAny(value, "\x00\r\n") || len(value) > 4096 {
				return errors.New("Dropbear UCI option value is unsafe")
			}
			verb := "set"
			if option.List {
				verb = "add_list"
			} else if index > 0 {
				return errors.New("Dropbear scalar UCI option is ambiguous")
			}
			fmt.Fprintf(&batch, "%s %s.%s.%s=%s\n", verb, dropbearConfigName, selector, name, quoteUCIValue(value))
		}
	}
	batch.WriteString("commit " + dropbearConfigName + "\n")
	_, err := h.run(ctx, h.uci, []byte(batch.String()), "-q", "batch")
	return err
}

func dropbearSelector(section uciSection) string {
	if !section.Anonymous && safeUCISectionRef(section.Name) {
		return section.Name
	}
	if section.Anonymous && section.TypeIndex >= 0 && section.TypeIndex < maxDropbearSections {
		// This is a freshly computed type index, never a stored identity or a
		// fixed @dropbear[0] assumption. The full config and unique endpoint
		// fingerprint are rechecked immediately before the bounded batch.
		return fmt.Sprintf("@dropbear[%d]", section.TypeIndex)
	}
	return ""
}

func quoteUCIValue(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func safeUCISectionRef(value string) bool {
	if value == "" || len(value) > 64 || strings.ContainsAny(value, ".@[]/\\\x00\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (h *dropbearUCIHost) selectedInstance(ctx context.Context, section uciSection) (dropbearInstanceEvidence, error) {
	serviceObservation, err := h.service.Observe(ctx)
	if err != nil {
		return dropbearInstanceEvidence{}, err
	}
	raw := serviceObservation.Evidence
	port, err := dropbearSectionPort(section)
	if err != nil {
		return dropbearInstanceEvidence{}, err
	}
	instance, err := parseDropbearProcdInstance(raw, section.Internal, port, h.dropbear.Label())
	if err != nil {
		return dropbearInstanceEvidence{}, err
	}
	process, err := processevidence.Observe(instance.PID)
	if err != nil || process.Executable != h.dropbear.Label() {
		return dropbearInstanceEvidence{}, errors.New("Dropbear procd process executable is unavailable")
	}
	instance.Start = process.StartTime
	instance.Revision = domain.Revision(struct {
		Raw, Name, Start string
		PID              int
	}{domain.Revision(raw), instance.Name, process.StartTime, instance.PID})
	return instance, nil
}

func parseDropbearProcdInstance(raw []byte, internal string, port uint16, executable string) (dropbearInstanceEvidence, error) {
	if len(raw) == 0 || len(raw) > maxCommandOutput || !safeUCISectionRef(internal) || port == 0 {
		return dropbearInstanceEvidence{}, errors.New("Dropbear procd evidence is malformed")
	}
	var services map[string]struct {
		Instances map[string]struct {
			Running bool     `json:"running"`
			PID     int      `json:"pid"`
			Command []string `json:"command"`
		} `json:"instances"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&services) != nil || len(services) != 1 || len(services[dropbearConfigName].Instances) > maxDropbearSections {
		return dropbearInstanceEvidence{}, errors.New("Dropbear procd service projection is ambiguous")
	}
	matches := make([]dropbearInstanceEvidence, 0, 1)
	pidFile := "/var/run/dropbear." + internal + ".pid"
	for name, value := range services[dropbearConfigName].Instances {
		// OpenWrt's Dropbear init script opens unnamed procd instances. The map
		// key is therefore procd-owned runtime metadata (for example instance1),
		// not the UCI section identity. The UCI section remains bound by its
		// section-derived pidfile; the surrounding observation then proves the
		// exact process generation, executable object, command, and sockets.
		if !safeUCISectionRef(name) || !value.Running || value.PID <= 1 || len(value.Command) < 4 || len(value.Command) > 64 || value.Command[0] != executable ||
			!argumentPair(value.Command, "-P", pidFile) || !commandHasDropbearPort(value.Command, port) {
			continue
		}
		matches = append(matches, dropbearInstanceEvidence{Name: name, PID: value.PID, Command: append([]string(nil), value.Command...)})
	}
	if len(matches) != 1 {
		return dropbearInstanceEvidence{}, errors.New("Dropbear UCI section does not select exactly one procd instance")
	}
	return matches[0], nil
}

func dropbearCommandCoversSocket(command []string, socket hostfacts.ListenerSocketIdentityV1) bool {
	for index := 0; index+1 < len(command); index++ {
		if command[index] != "-p" {
			continue
		}
		bind, port, _, err := parseDropbearListenSpec(command[index+1])
		if err != nil || port != socket.Port {
			continue
		}
		if !bind.IsValid() && socket.Wildcard || bind.IsValid() && bind.Is4() == (socket.Family == hostfacts.FamilyIPv4) && bind.String() == socket.Bind {
			return true
		}
	}
	return false
}

func dropbearCommandExactlyCoversSockets(command []string, sockets []hostfacts.ListenerSocketIdentityV1) bool {
	expected, err := dropbearCommandEndpointSet(command)
	if err != nil {
		return false
	}
	observed := make(map[string]bool, len(sockets)*2)
	for _, socket := range sockets {
		if !dropbearCommandCoversSocket(command, socket) {
			return false
		}
		for _, covered := range socket.CoverageFamilies {
			family := hostresources.AddressFamily(covered)
			bind, ok := dropbearEndpointBind(socket, family)
			if !ok {
				return false
			}
			key := dropbearRuntimeEndpointKey(family, bind, socket.Port)
			if observed[key] {
				return false
			}
			observed[key] = true
		}
	}
	if len(observed) != len(expected) {
		return false
	}
	for key := range expected {
		if !observed[key] {
			return false
		}
	}
	return true
}

func dropbearCommandEndpointSet(command []string) (map[string]bool, error) {
	result := map[string]bool{}
	for index := 0; index+1 < len(command); index++ {
		if command[index] != "-p" {
			continue
		}
		bind, port, _, err := parseDropbearListenSpec(command[index+1])
		if err != nil {
			return nil, err
		}
		endpoints := []struct {
			family hostresources.AddressFamily
			bind   string
		}{}
		if !bind.IsValid() {
			endpoints = append(endpoints,
				struct {
					family hostresources.AddressFamily
					bind   string
				}{hostresources.AddressFamilyIPv4, "0.0.0.0"},
				struct {
					family hostresources.AddressFamily
					bind   string
				}{hostresources.AddressFamilyIPv6, "::"},
			)
		} else {
			family := hostresources.AddressFamilyIPv6
			if bind.Is4() {
				family = hostresources.AddressFamilyIPv4
			}
			endpoints = append(endpoints, struct {
				family hostresources.AddressFamily
				bind   string
			}{family, bind.String()})
		}
		for _, endpoint := range endpoints {
			key := dropbearRuntimeEndpointKey(endpoint.family, endpoint.bind, port)
			if result[key] {
				return nil, errors.New("Dropbear procd command contains duplicate listen authority")
			}
			result[key] = true
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Dropbear procd command has no listen authority")
	}
	return result, nil
}

func dropbearRuntimeEndpointKey(family hostresources.AddressFamily, bind string, port uint16) string {
	return string(family) + "\x00" + bind + "\x00" + strconv.Itoa(int(port))
}

// parseDropbearListenSpec mirrors Dropbear's split_address_port grammar. In
// particular, OpenWrt emits address-qualified IPv6 listeners without brackets
// ("2001:db8::1:22"), which Dropbear splits at the last colon.
func parseDropbearListenSpec(value string) (netip.Addr, uint16, bool, error) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n\t ") {
		return netip.Addr{}, 0, false, errors.New("Dropbear listen argument is malformed")
	}
	addressText, portText := "", value
	if value[0] == '[' {
		closing := strings.IndexByte(value, ']')
		if closing <= 1 || closing+1 >= len(value) || value[closing+1] != ':' {
			return netip.Addr{}, 0, false, errors.New("Dropbear bracketed listen argument is malformed")
		}
		addressText, portText = value[1:closing], value[closing+2:]
		if strings.Contains(value[closing+2:], "]") {
			return netip.Addr{}, 0, false, errors.New("Dropbear bracketed listen argument is malformed")
		}
	} else if separator := strings.LastIndexByte(value, ':'); separator >= 0 {
		addressText, portText = value[:separator], value[separator+1:]
	}
	parsedPort, err := parseDropbearUnsigned(portText, 16)
	if err != nil || parsedPort == 0 {
		return netip.Addr{}, 0, false, errors.New("Dropbear listen port is invalid")
	}
	if addressText == "" {
		return netip.Addr{}, uint16(parsedPort), true, nil
	}
	address, err := netip.ParseAddr(addressText)
	if err != nil || address.Zone() != "" || address.Is4In6() {
		return netip.Addr{}, 0, false, errors.New("Dropbear listen address is invalid")
	}
	return address, uint16(parsedPort), address.IsUnspecified(), nil
}

func argumentPair(command []string, flag, expected string) bool {
	for index := 0; index+1 < len(command); index++ {
		if command[index] == flag && command[index+1] == expected {
			return true
		}
	}
	return false
}

func commandHasDropbearPort(command []string, port uint16) bool {
	for index := 0; index+1 < len(command); index++ {
		if command[index] != "-p" {
			continue
		}
		_, observed, _, err := parseDropbearListenSpec(command[index+1])
		if err == nil && observed == port {
			return true
		}
	}
	return false
}

func (h *dropbearUCIHost) acceptedPublicKeyAt(output []byte, username string, remote netip.AddrPort, daemonPID int, now time.Time) (int64, error) {
	return parseDropbearAuthLog(output, username, remote, daemonPID, now)
}

func parseDropbearAuthLog(data []byte, username string, remote netip.AddrPort, daemonPID int, now time.Time) (int64, error) {
	if len(data) == 0 || len(data) > maxCommandOutput || bytes.IndexByte(data, 0) >= 0 || !safeToken(username, 128) || !remote.IsValid() || daemonPID <= 1 {
		return 0, errors.New("Dropbear security-log evidence is malformed")
	}
	latestMillis := int64(0)
	records := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), maxCommandOutput)
	for scanner.Scan() {
		records++
		if records > maxDropbearLogRecords {
			return 0, errors.New("Dropbear security-log record count exceeded its bound")
		}
		line := scanner.Text()
		if len(line) > 4096 {
			return 0, errors.New("Dropbear security-log record exceeded its bound")
		}
		match := dropbearLogPattern.FindStringSubmatch(line)
		if match == nil || match[4] != username {
			continue
		}
		pid, _ := strconv.Atoi(match[3])
		address, err := netip.ParseAddrPort(match[5])
		if err != nil || pid != daemonPID || address.Addr().Unmap() != remote.Addr().Unmap() || address.Port() != remote.Port() {
			continue
		}
		seconds, _ := strconv.ParseInt(match[1], 10, 64)
		milliseconds, _ := strconv.ParseInt(match[2], 10, 64)
		observedMillis := seconds*1000 + milliseconds
		if observedMillis <= now.Add(-11*time.Minute).UnixMilli() || observedMillis > now.Add(5*time.Minute).UnixMilli() {
			continue
		}
		if observedMillis > latestMillis {
			latestMillis = observedMillis
		}
	}
	if scanner.Err() != nil || latestMillis == 0 {
		return 0, errors.New("fresh Dropbear public-key evidence is absent")
	}
	return latestMillis, nil
}

func (h *dropbearUCIHost) run(ctx context.Context, object sshExecutableObject, stdin []byte, args ...string) ([]byte, error) {
	return runSSHCapabilityInputCommand(ctx, object, stdin, args...)
}

func dropbearPrior(content []byte, digest string) PriorArtifactV1 {
	return PriorArtifactV1{Present: true, Content: append([]byte(nil), content...), Owner: "root", Group: "root",
		ModeClass: "owner_read_write", Mode: 0o600, Digest: digest}
}

func dropbearArtifactDigest(section uciSection) string {
	if value, ok := optionScalar(section, dropbearManagedMarker); ok && digest(value) {
		return value
	}
	return domain.Revision(struct {
		Type, Name string
		Anonymous  bool
		Options    map[string]uciOption
	}{section.Type, section.Name, section.Anonymous, section.Options})
}

func dropbearSectionFingerprint(section uciSection) string {
	return domain.Revision(struct {
		Name, Internal, Type string
		TypeIndex            int
		Anonymous            bool
		Options              map[string]uciOption
	}{section.Name, section.Internal, section.Type, section.TypeIndex, section.Anonymous, section.Options})
}

func cloneUCISection(value uciSection) uciSection {
	result := value
	result.Options = make(map[string]uciOption, len(value.Options))
	for name, option := range value.Options {
		option.Values = append([]string(nil), option.Values...)
		result.Options[name] = option
	}
	return result
}

func dropbearOptionsEqual(left, right map[string]uciOption) bool {
	return domain.Revision(left) == domain.Revision(right)
}

func dropbearUnrelatedSectionsEqual(before, after dropbearConfig, selectedInternal string) bool {
	filter := func(config dropbearConfig) []uciSection {
		result := make([]uciSection, 0, len(config.Sections)-1)
		for _, section := range config.Sections {
			if section.Internal != selectedInternal {
				result = append(result, section)
			}
		}
		return result
	}
	return domain.Revision(filter(before)) == domain.Revision(filter(after))
}

func dropbearVersionClass(output string) string {
	value := strings.TrimSpace(output)
	if index := strings.Index(value, "v"); index >= 0 {
		value = value[index+1:]
	}
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' {
			return r
		}
		return -1
	}, value)
	if value == "" || len(value) > 48 {
		return "dropbear_unknown"
	}
	return "dropbear_" + strings.ReplaceAll(value, ".", "_")
}

type dropbearRuntimeSemantics struct {
	password, rootPassword, rootLogin bool
	local, remote, gateway            bool
	maxAuthTries                      uint16
}

func projectDropbearRuntimePosture(sections []uciSection, instances []dropbearInstanceEvidence) (domain.AuthenticationPostureV1, domain.ForwardingPostureV1, error) {
	if len(sections) == 0 || len(sections) != len(instances) {
		return domain.AuthenticationPostureV1{}, domain.ForwardingPostureV1{}, errors.New("Dropbear runtime/configuration projection is incomplete")
	}
	password, local, remote, gateway := false, false, false, false
	rootPolicy := "no"
	maxTries := uint16(1)
	for index := range sections {
		runtime, err := parseDropbearOpenWrtCommand(instances[index].Command)
		if err != nil {
			return domain.AuthenticationPostureV1{}, domain.ForwardingPostureV1{}, err
		}
		if err := validateDropbearRuntimeAgainstSection(sections[index], runtime); err != nil {
			return domain.AuthenticationPostureV1{}, domain.ForwardingPostureV1{}, err
		}
		password = password || runtime.password
		local = local || runtime.local
		remote = remote || runtime.remote
		gateway = gateway || runtime.gateway
		if runtime.maxAuthTries > maxTries {
			maxTries = runtime.maxAuthTries
		}
		if runtime.rootLogin && runtime.rootPassword && runtime.password {
			rootPolicy = "yes"
		} else if runtime.rootLogin && rootPolicy == "no" {
			rootPolicy = "prohibit-password"
		}
	}
	methods := []string{"publickey"}
	if password {
		methods = append(methods, "password")
	}
	authentication := domain.AuthenticationPostureV1{PasswordAuthentication: yesNoDropbear(password), KbdInteractiveAuthentication: "no",
		PermitRootLogin: rootPolicy, PubkeyAuthentication: "yes", AuthenticationMethods: methods,
		MaxAuthTries: maxTries, LoginGraceTimeSeconds: 0, MaxStartupsClass: "dropbear_default"}
	tcp := "no"
	if local && remote {
		tcp = "yes"
	} else if local {
		tcp = "local"
	} else if remote {
		tcp = "remote"
	}
	forwarding := domain.ForwardingPostureV1{AllowAgentForwarding: "yes", AllowTCPForwarding: tcp,
		GatewayPorts: yesNoDropbear(gateway), PermitTunnel: "no", X11Forwarding: "no"}
	return authentication, forwarding, nil
}

func parseDropbearOpenWrtCommand(command []string) (dropbearRuntimeSemantics, error) {
	if len(command) < 4 || len(command) > 64 || command[0] != "/usr/sbin/dropbear" {
		return dropbearRuntimeSemantics{}, errors.New("Dropbear procd command is malformed")
	}
	flags := map[string]int{}
	values := map[string][]string{}
	for index := 1; index < len(command); index++ {
		flag := command[index]
		switch flag {
		case "-F", "-s", "-a", "-j", "-k", "-g", "-w":
			flags[flag]++
			if flags[flag] > 1 {
				return dropbearRuntimeSemantics{}, errors.New("Dropbear procd command contains a duplicate flag")
			}
		case "-P", "-l", "-p", "-c", "-r", "-b", "-I", "-K", "-T", "-W":
			if index+1 >= len(command) || command[index+1] == "" || strings.ContainsAny(command[index+1], "\x00\r\n") {
				return dropbearRuntimeSemantics{}, errors.New("Dropbear procd command option is incomplete")
			}
			index++
			values[flag] = append(values[flag], command[index])
			if flag != "-p" && flag != "-r" && len(values[flag]) > 1 {
				return dropbearRuntimeSemantics{}, errors.New("Dropbear procd command contains a duplicate option")
			}
		default:
			return dropbearRuntimeSemantics{}, errors.New("Dropbear procd command contains an unsupported argument")
		}
	}
	if flags["-F"] != 1 || len(values["-P"]) != 1 || len(values["-p"]) == 0 || len(values["-T"]) != 1 {
		return dropbearRuntimeSemantics{}, errors.New("Dropbear procd command lacks required effective settings")
	}
	for _, value := range values["-p"] {
		if _, _, _, err := parseDropbearListenSpec(value); err != nil {
			return dropbearRuntimeSemantics{}, err
		}
	}
	tries, err := parseDropbearUnsigned(values["-T"][0], 16)
	if err != nil || tries < 1 || tries > 20 {
		return dropbearRuntimeSemantics{}, errors.New("Dropbear effective MaxAuthTries is unsupported")
	}
	return dropbearRuntimeSemantics{
		password:     flags["-s"] == 0,
		rootPassword: flags["-g"] == 0,
		rootLogin:    flags["-w"] == 0,
		local:        flags["-j"] == 0,
		remote:       flags["-k"] == 0,
		gateway:      flags["-a"] == 1,
		maxAuthTries: uint16(tries),
	}, nil
}

func validateDropbearRuntimeAgainstSection(section uciSection, runtime dropbearRuntimeSemantics) error {
	expectations := []struct {
		name     string
		fallback bool
		observed bool
	}{
		{"PasswordAuth", true, runtime.password},
		{"RootPasswordAuth", true, runtime.rootPassword},
		{"RootLogin", true, runtime.rootLogin},
		{"LocalPortForward", true, runtime.local},
		{"RemotePortForward", true, runtime.remote},
		{"GatewayPorts", false, runtime.gateway},
	}
	for _, expectation := range expectations {
		expected, err := dropbearBoolOption(section, expectation.name, expectation.fallback)
		if err != nil || expected != expectation.observed {
			return errors.New("Dropbear UCI and procd security settings disagree")
		}
	}
	value, present, err := strictScalarOption(section, "MaxAuthTries")
	if err != nil {
		return errors.New("Dropbear MaxAuthTries configuration is malformed")
	}
	expectedTries := uint64(3) // validate_section_dropbear's OpenWrt default.
	if present {
		expectedTries, err = parseDropbearUnsigned(value, 16)
		if err != nil || expectedTries == 0 || expectedTries > 20 {
			return errors.New("Dropbear MaxAuthTries configuration is unsupported")
		}
	}
	if uint16(expectedTries) != runtime.maxAuthTries {
		return errors.New("Dropbear UCI and procd MaxAuthTries disagree")
	}
	return nil
}

func yesNoDropbear(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func dropbearBoolOption(section uciSection, name string, fallback bool) (bool, error) {
	value, ok, err := strictScalarOption(section, name)
	if err != nil {
		return false, errors.New("Dropbear boolean option is malformed")
	}
	if !ok {
		return fallback, nil
	}
	parsed, valid := parseUCIBool(value)
	if !valid {
		return false, errors.New("Dropbear boolean option is invalid")
	}
	return parsed, nil
}

func dropbearAuthorizedKeys() domain.AuthorizedKeysPostureV1 {
	data, _, _, err := secureRootFile("/etc/dropbear/authorized_keys", 1<<20)
	if err != nil {
		return domain.AuthorizedKeysPostureV1{StrictModes: "yes"}
	}
	return domain.AuthorizedKeysPostureV1{StrictModes: "yes", PathTemplateCount: 1, PathTemplateRevision: domain.Revision(data)}
}

func dropbearHostKeys() []domain.HostKeyPostureV1 {
	paths, _ := filepath.Glob("/etc/dropbear/dropbear_*_host_key")
	result := make([]domain.HostKeyPostureV1, 0, len(paths))
	for _, path := range paths {
		data, info, _, err := secureRootFile(path, 1<<20)
		if err != nil || len(data) == 0 {
			continue
		}
		kind := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "dropbear_"), "_host_key")
		if !safeToken(kind, 64) {
			continue
		}
		result = append(result, domain.HostKeyPostureV1{Type: kind, Fingerprint: domain.Revision(data), Count: 1,
			Owner: "root", ModeClass: configModeClass(info.Mode().Perm())})
	}
	return result
}
