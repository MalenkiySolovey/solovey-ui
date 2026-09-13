//go:build linux

package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	processevidence "github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/systemdexec"
	"golang.org/x/sys/unix"
)

const (
	maxOwnerUnitBytes = int64(1 << 20)
)

type systemdListenerOwnerExecutor struct {
	systemctl *systemdexec.Object
}

func newComposedListenerOwnerExecutor() ListenerOwnerExecutor {
	return supervisorListenerOwnerExecutor{}
}

func init() { registerListenerOwnerProofAdapter(systemdListenerOwnerExecutor{}) }

func (e systemdListenerOwnerExecutor) Detect(context.Context) ListenerOwnerSupport {
	contract, err := deploymentidentity.LoadInstalled()
	if err != nil {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_contract_unavailable", ObserverRevision: listenerOwnerObserverDigest()}
	}
	systemctl, owned, err := e.openSystemctl()
	if owned && systemctl != nil {
		defer systemctl.Close()
	}
	if err != nil || systemctl.Revalidate() != nil {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_systemd_unavailable", ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
	}
	return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Available: true, ContractRevision: contract.Revision, ObserverRevision: listenerOwnerObserverDigest()}
}

func (e systemdListenerOwnerExecutor) Observe(ctx context.Context, request ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error) {
	result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}}
	contract, err := deploymentidentity.LoadInstalled()
	if err != nil {
		result.ReasonCodes = []string{"listener_owner_contract_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	if !ownerRequestMatchesSystemdContract(request, contract) {
		result.ReasonCodes = []string{"listener_deployment_mismatch"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	systemctl, owned, err := e.openSystemctl()
	if owned && systemctl != nil {
		defer systemctl.Close()
	}
	if err != nil {
		result.ReasonCodes = []string{"listener_owner_systemd_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	before, err := observeSystemdService(ctx, systemctl, contract)
	if err != nil {
		result.ReasonCodes = []string{"listener_service_unavailable"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	process, err := observeExactSystemdProcess(ctx, before.MainPID, contract)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		result.ReasonCodes = []string{"listener_process_identity_mismatch"}
		sealListenerOwnerResult(result)
		return result, nil
	}
	sockets, reasons, err := observeRequestListeners(ctx, before.MainPID, request)
	if err != nil {
		return nil, err
	}
	result.ReasonCodes = append(result.ReasonCodes, reasons...)
	after, err := observeSystemdService(ctx, systemctl, contract)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if before != after {
		result.ReasonCodes = append(result.ReasonCodes, "listener_owner_stale")
		sealListenerOwnerResult(result)
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	currentStart, err := readProcessStart(before.MainPID)
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
	pid := before.MainPID
	service := hostfacts.ServiceFact{
		SupervisorRevision: listenerOwnerProjectionRevision(struct {
			Kind, Contract, Process string
		}{"systemd", contract.Revision, process.EvidenceRevision}),
		CgroupAvailability: "available", CgroupPolicy: "required",
		CgroupRevision: listenerOwnerProjectionRevision(struct {
			Provider, Availability, Policy, Path string
		}{processevidence.RevisionV2, "available", "required", before.ControlGroup}),
		SystemdUnit: contract.SystemdUnit, MainPID: &pid, FragmentPath: before.FragmentPath,
		FragmentSHA256: before.FragmentSHA256,
		ActiveState:    before.ActiveState, SubState: before.SubState, ControlGroup: before.ControlGroup,
		StartMonotonicUsec: before.StartMonotonicUsec,
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

func (e systemdListenerOwnerExecutor) openSystemctl() (*systemdexec.Object, bool, error) {
	if e.systemctl != nil {
		return e.systemctl, false, e.systemctl.Revalidate()
	}
	object, err := systemdexec.Open()
	return object, true, err
}

func ownerRequestMatchesSystemdContract(request ListenerOwnerObserveRequest, contract deploymentidentity.ApplicationOwnerContractV1) bool {
	return request.ExpectedInstanceID == contract.InstanceID && request.ExpectedSourceRevision == contract.SourceRevision &&
		request.ExpectedArtifactRevision == contract.ArtifactRevision && request.ExpectedDeploymentID == contract.DeploymentID &&
		request.ExpectedOwnerContractRevision == contract.Revision && request.ExpectedRuntimeRootBindingRevision == contract.RuntimeRootBindingRevision
}

type systemdOwnerSnapshot struct {
	MainPID            int
	ActiveState        string
	SubState           string
	ControlGroup       string
	FragmentPath       string
	FragmentSHA256     string
	StartMonotonicUsec uint64
}

func observeSystemdService(ctx context.Context, systemctl *systemdexec.Object, contract deploymentidentity.ApplicationOwnerContractV1) (systemdOwnerSnapshot, error) {
	if systemctl == nil || systemctl.File() == nil || systemctl.Revalidate() != nil {
		return systemdOwnerSnapshot{}, errors.New("systemd executable authority is unavailable")
	}
	properties := []string{"MainPID", "ActiveState", "SubState", "ControlGroup", "FragmentPath", "ExecMainStartTimestampMonotonic"}
	arguments := []string{"show", contract.SystemdUnit, "--no-page"}
	for _, property := range properties {
		arguments = append(arguments, "--property="+property)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, systemctl.ExecPath(0), arguments...)
	command.Args[0] = systemctl.Label()
	command.ExtraFiles = []*os.File{systemctl.File()}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	stdout, stderr := &boundedBuffer{limit: MaxOutputBytes}, &boundedBuffer{limit: MaxOutputBytes}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil || stdout.truncated || stderr.truncated {
		return systemdOwnerSnapshot{}, errors.New("bounded systemd owner observation failed")
	}
	values := map[string]string{}
	for _, line := range strings.Split(stdout.buffer.String(), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || values[key] != "" {
			continue
		}
		values[key] = value
	}
	mainPID, err := strconv.Atoi(values["MainPID"])
	start, startErr := strconv.ParseUint(values["ExecMainStartTimestampMonotonic"], 10, 64)
	resolvedFragment, fragmentErr := filepath.EvalSymlinks(values["FragmentPath"])
	if err != nil || startErr != nil || mainPID <= 1 || values["ActiveState"] != "active" || values["SubState"] != "running" ||
		values["ControlGroup"] != contract.ServiceControlGroup || fragmentErr != nil || resolvedFragment != contract.ServiceFragmentPath {
		return systemdOwnerSnapshot{}, errors.New("systemd service identity differs")
	}
	fragmentSHA, err := observeSystemdServiceFragment(contract)
	if err != nil {
		return systemdOwnerSnapshot{}, err
	}
	return systemdOwnerSnapshot{mainPID, values["ActiveState"], values["SubState"], values["ControlGroup"], resolvedFragment, fragmentSHA, start}, nil
}

func observeSystemdServiceFragment(contract deploymentidentity.ApplicationOwnerContractV1) (string, error) {
	before := unix.Stat_t{}
	if err := unix.Lstat(contract.ServiceFragmentPath, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Mode&0o022 != 0 || before.Size <= 0 || before.Size > maxOwnerUnitBytes {
		return "", errors.New("service unit fragment is unsafe")
	}
	file, err := os.Open(contract.ServiceFragmentPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	after := unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), &after); err != nil || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim {
		return "", errors.New("service unit fragment changed while opening")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxOwnerUnitBytes+1))
	if err != nil || written != before.Size || written > maxOwnerUnitBytes {
		return "", errors.New("service unit fragment read failed")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if digest != contract.ServiceUnitSHA256 {
		return "", errors.New("service unit fragment identity differs")
	}
	return digest, nil
}

func observeExactSystemdProcess(ctx context.Context, pid int, contract deploymentidentity.ApplicationOwnerContractV1) (hostfacts.ProcessFact, error) {
	evidence, err := processevidence.Observe(pid)
	if err != nil {
		return hostfacts.ProcessFact{}, err
	}
	if uint32(evidence.UID) != contract.ProcessUID || uint32(evidence.GID) != contract.ProcessGID {
		return hostfacts.ProcessFact{}, errors.New("process credentials differ")
	}
	cgroup, available := processevidence.UnifiedCgroup(evidence)
	if evidence.CgroupAvailability != processevidence.CgroupAvailable || !available || cgroup != contract.ServiceControlGroup {
		return hostfacts.ProcessFact{}, errors.New("process cgroup differs")
	}
	if evidence.ExeMode&unix.S_IFMT != unix.S_IFREG || evidence.ExeMode&0o111 == 0 || evidence.ExeMode&0o022 != 0 ||
		evidence.ExeUID != 0 || evidence.ExeGID != 0 || evidence.ExeSize <= 0 || evidence.ExeDevice == 0 || evidence.ExeInode == 0 ||
		evidence.ExeDigest != contract.ExecutableSHA256 {
		return hostfacts.ProcessFact{}, errors.New("process executable is not a regular file")
	}
	if err := ctx.Err(); err != nil {
		return hostfacts.ProcessFact{}, err
	}
	return hostfacts.ProcessFact{
		ProviderRevision: evidence.ProviderRevision, EvidenceRevision: evidence.Revision,
		PID: &pid, ParentPID: &evidence.ParentPID, SessionID: &evidence.SessionID, StartTime: evidence.StartTime,
		ExeDigest: evidence.ExeDigest, Executable: evidence.Executable, ExeDevice: evidence.ExeDevice, ExeInode: evidence.ExeInode,
		UID: &evidence.UID, GID: &evidence.GID, ControlGroup: cgroup,
	}, nil
}

func readProcessStart(pid int) (string, error) {
	evidence, err := processevidence.Observe(pid)
	return evidence.StartTime, err
}

func observeRequestListeners(ctx context.Context, pid int, request ListenerOwnerObserveRequest) ([]hostfacts.ListenerSocketIdentityV1, []string, error) {
	observation, err := listenerevidence.ObserveProcessSockets(ctx, pid, hostfacts.Network(request.Network), map[uint16]bool{uint16(request.Port): true})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, nil, err
		}
		reason, _, _ := listenerevidence.DiagnosticOf(err)
		switch reason {
		case listenerevidence.ReasonDescriptorBound:
			return nil, []string{"listener_owner_scan_bounded"}, nil
		case listenerevidence.ReasonProcessChanged, listenerevidence.ReasonObservationChanged:
			return nil, []string{"listener_owner_stale"}, nil
		case listenerevidence.ReasonPIDFDAuthority:
			return nil, []string{"listener_owner_capability_unavailable"}, nil
		default:
			return nil, []string{"listener_owner_unavailable"}, nil
		}
	}
	result := make([]hostfacts.ListenerSocketIdentityV1, 0, len(observation.Sockets))
	for _, socket := range observation.Sockets {
		address, parseErr := netip.ParseAddr(socket.Bind)
		if parseErr == nil && socket.Port == uint16(request.Port) && listenerAddressMatches(request, address) {
			result = append(result, socket)
		}
	}
	return result, nil, nil
}

func listenerAddressMatches(request ListenerOwnerObserveRequest, observed netip.Addr) bool {
	if request.ConfiguredMode == "exact" {
		expected, err := netip.ParseAddr(request.ConfiguredAddress)
		return err == nil && expected == observed
	}
	if !observed.IsUnspecified() {
		return false
	}
	switch request.ConfiguredAddress {
	case "*":
		return true
	case "0.0.0.0":
		return observed.Is4()
	case "::":
		return observed.Is6()
	default:
		return false
	}
}

func duplicateOwnerCoverage(sockets []hostfacts.ListenerSocketIdentityV1) bool {
	seen := map[string]bool{}
	for _, socket := range sockets {
		for _, family := range socket.CoverageFamilies {
			key := string(socket.Network) + "\x00" + string(family) + "\x00" + strconv.Itoa(int(socket.Port))
			if seen[key] {
				return true
			}
			seen[key] = true
		}
	}
	return false
}
