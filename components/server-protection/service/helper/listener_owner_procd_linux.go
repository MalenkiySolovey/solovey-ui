//go:build linux

package helper

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

type procdListenerOwnerExecutor struct{}

func init() { registerListenerOwnerProofAdapter(procdListenerOwnerExecutor{}) }

func (procdListenerOwnerExecutor) Detect(ctx context.Context) ListenerOwnerSupport {
	contract, client, err := installedProcdOwner()
	if err != nil {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_procd_unavailable", ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
	}
	if _, err := broker.NewProcdInspector().Inspect(ctx, client); err != nil {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_procd_unavailable", ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
	}
	return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Available: true, ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
}

func (procdListenerOwnerExecutor) Observe(ctx context.Context, request ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error) {
	result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}}
	contract, client, err := installedProcdOwner()
	if err != nil {
		result.ReasonCodes = []string{"listener_owner_contract_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	if !ownerRequestMatchesProcdContract(request, contract) {
		result.ReasonCodes = []string{"listener_deployment_mismatch"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	inspector := broker.NewProcdInspector()
	before, err := inspector.Inspect(ctx, client)
	if err != nil {
		result.ReasonCodes = []string{"listener_service_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	process, err := observeExactProcdProcess(ctx, before.PID, contract, client)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		result.ReasonCodes = []string{"listener_process_identity_mismatch"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	sockets, reasons, err := observeRequestListeners(ctx, before.PID, request)
	if err != nil {
		return nil, err
	}
	result.ReasonCodes = append(result.ReasonCodes, reasons...)
	after, err := inspector.Inspect(ctx, client)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if !sameProcdEvidence(before, after) {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	currentStart, err := readProcessStart(before.PID)
	if err != nil || currentStart != process.StartTime {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if len(sockets) == 0 {
		if len(result.ReasonCodes) == 0 {
			result.ReasonCodes = append(result.ReasonCodes, "listener_unobserved")
		}
		sealListenerOwnerResult(result)
		return result, nil
	}
	if duplicateOwnerCoverage(sockets) {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_ambiguous")
		sealListenerOwnerResult(result)
		return result, nil
	}
	now := time.Now().UTC()
	pid := before.PID
	cgroupAvailability := "unavailable"
	if process.ControlGroup != "" {
		cgroupAvailability = "available"
	}
	service := hostfacts.ServiceFact{
		SupervisorRevision: listenerOwnerProjectionRevision(struct {
			Kind, Contract, Service, Instance, Process string
		}{"procd", contract.Revision, client.ProcdService, client.ProcdInstance, process.EvidenceRevision}),
		CgroupAvailability: cgroupAvailability, CgroupPolicy: "optional",
		CgroupRevision: listenerOwnerProjectionRevision(struct {
			Provider, Availability, Policy, Path, Service, Instance string
		}{processevidence.RevisionV2, cgroupAvailability, "optional", process.ControlGroup, client.ProcdService, client.ProcdInstance}),
		ControlGroup: process.ControlGroup,
		MainPID:      &pid, ActiveState: "active", SubState: "running",
		ProcdService: client.ProcdService, ProcdInstance: client.ProcdInstance,
		ProcdCommand: append([]string(nil), before.Command...), ProcdUser: before.User, ProcdGroup: before.Group,
	}
	for _, socket := range sockets {
		fact := hostfacts.ListenerOwnerFactV1{
			Schema: hostfacts.ListenerOwnerFactSchemaV1, Socket: socket, Process: process, Service: service,
			Application: hostfacts.ListenerApplicationIdentityV1{
				InstanceID: contract.InstanceID, SourceRevision: contract.SourceRevision,
				ArtifactRevision: contract.ArtifactRevision, DeploymentID: contract.DeploymentID,
				OwnerContractRevision: contract.Revision, RuntimeRootBindingRevision: contract.RuntimeRootBindingRevision,
				ExpectedExecutableSHA256: contract.ExecutableSHA256, ServiceIdentity: contract.ServiceIdentity,
				ResourceID: request.ResourceID, ResourceOwnerRevision: request.ExpectedResourceOwnerRevision,
				ConfigurationRevision: request.ExpectedConfigurationRevision,
			},
			ObservedAt: now.Unix(), ExpiresAt: now.Add(hostfacts.MaxListenerOwnerFactLifetime).Unix(),
		}
		fact.Seal()
		result.Facts = append(result.Facts, fact)
	}
	sealListenerOwnerResult(result)
	return result, nil
}

func installedProcdOwner() (deploymentidentity.ApplicationOwnerContractProcdV1, broker.ClientManifest, error) {
	contract, err := deploymentidentity.LoadProcdInstalled()
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, broker.ClientManifest{}, err
	}
	manifest, err := broker.LoadManifest(broker.RuntimeManifestPath)
	if err != nil || !manifest.RequiresProcd() || manifest.ApplicationOwnerRevision != contract.Revision {
		return contract, broker.ClientManifest{}, errors.New("procd listener owner manifest differs")
	}
	client, err := procdPanelManifestClient(manifest, contract)
	return contract, client, err
}

func procdPanelManifestClient(manifest broker.Manifest, contract deploymentidentity.ApplicationOwnerContractProcdV1) (broker.ClientManifest, error) {
	var selected broker.ClientManifest
	matches := 0
	for _, client := range manifest.Clients {
		panelRole := len(client.Roles) == 1 && client.Roles[0] == broker.RolePanel
		if panelRole && client.ProcdRelation == broker.ProcdRelationMain && client.ProcdService == contract.ProcdService && client.ProcdInstance == contract.ProcdInstance &&
			client.Executable == contract.ExecutablePath && client.ExecutableDigest == contract.ExecutableSHA256 &&
			client.UID == contract.ProcessUID && client.GID == contract.ProcessGID && !client.AnyNonRootUID && !client.AnyGID &&
			client.CgroupPolicy == broker.CgroupOptional && client.CgroupAuthorityRevision == broker.CgroupAuthorityRevisionV1 {
			selected, matches = client, matches+1
		}
	}
	if matches != 1 {
		return broker.ClientManifest{}, errors.New("procd panel manifest identity is ambiguous")
	}
	return selected, nil
}

func ownerRequestMatchesProcdContract(request ListenerOwnerObserveRequest, contract deploymentidentity.ApplicationOwnerContractProcdV1) bool {
	return request.ExpectedInstanceID == contract.InstanceID && request.ExpectedSourceRevision == contract.SourceRevision &&
		request.ExpectedArtifactRevision == contract.ArtifactRevision && request.ExpectedDeploymentID == contract.DeploymentID &&
		request.ExpectedOwnerContractRevision == contract.Revision && request.ExpectedRuntimeRootBindingRevision == contract.RuntimeRootBindingRevision
}

func observeExactProcdProcess(ctx context.Context, pid int, contract deploymentidentity.ApplicationOwnerContractProcdV1, client broker.ClientManifest) (hostfacts.ProcessFact, error) {
	evidence, err := processevidence.Observe(pid)
	if err != nil {
		return hostfacts.ProcessFact{}, err
	}
	if uint32(evidence.UID) != contract.ProcessUID || uint32(evidence.GID) != contract.ProcessGID || client.UID != contract.ProcessUID || client.GID != contract.ProcessGID {
		return hostfacts.ProcessFact{}, errors.New("process credentials differ")
	}
	if client.Executable != contract.ExecutablePath || evidence.ExeMode&unix.S_IFMT != unix.S_IFREG || evidence.ExeMode&0o111 == 0 ||
		evidence.ExeMode&0o022 != 0 || evidence.ExeUID != 0 || evidence.ExeGID != 0 || evidence.ExeSize <= 0 ||
		evidence.ExeDevice != client.Device || evidence.ExeInode != client.Inode || evidence.ExeDigest != contract.ExecutableSHA256 ||
		evidence.ExeDigest != client.ExecutableDigest {
		return hostfacts.ProcessFact{}, errors.New("process executable is not the manifest executable")
	}
	if err := ctx.Err(); err != nil {
		return hostfacts.ProcessFact{}, err
	}
	cgroup := ""
	switch evidence.CgroupAvailability {
	case processevidence.CgroupAvailable:
		observed, available := processevidence.UnifiedCgroup(evidence)
		if !available {
			return hostfacts.ProcessFact{}, errors.New("process procd cgroup evidence is malformed")
		}
		expected := "/services/" + client.ProcdService + "/" + client.ProcdInstance
		switch {
		case observed == expected:
			cgroup = observed
		case strings.HasPrefix(observed, "/services/"):
			return hostfacts.ProcessFact{}, errors.New("process procd cgroup identity differs")
		}
	case processevidence.CgroupUnavailable:
		if client.CgroupPolicy != broker.CgroupOptional || client.CgroupAuthorityRevision != broker.CgroupAuthorityRevisionV1 {
			return hostfacts.ProcessFact{}, errors.New("required process procd cgroup evidence is unavailable")
		}
	default:
		return hostfacts.ProcessFact{}, errors.New("process procd cgroup evidence is unsafe")
	}
	return hostfacts.ProcessFact{
		ProviderRevision: evidence.ProviderRevision, EvidenceRevision: evidence.Revision,
		PID: &pid, ParentPID: &evidence.ParentPID, SessionID: &evidence.SessionID, StartTime: evidence.StartTime,
		ExeDigest: evidence.ExeDigest, Executable: evidence.Executable, ExeDevice: evidence.ExeDevice, ExeInode: evidence.ExeInode,
		UID: &evidence.UID, GID: &evidence.GID, ControlGroup: cgroup,
	}, nil
}

func sameProcdEvidence(left, right broker.ProcdInstanceEvidence) bool {
	return left.PID == right.PID && left.User == right.User && left.Group == right.Group && slices.Equal(left.Command, right.Command)
}
