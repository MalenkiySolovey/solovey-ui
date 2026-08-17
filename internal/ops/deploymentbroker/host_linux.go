//go:build linux

package deploymentbroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"golang.org/x/sys/unix"
)

const (
	activeUnit     = "/etc/systemd/system/solovey-ui.service"
	profileRoot    = "/usr/local/lib/solovey-ui/systemd"
	profileMarker  = "/etc/solovey-ui/deployment-profile"
	checkpointRoot = "/var/lib/solovey-ui-broker/deployment"
	maxCheckpoints = 256
	legacyDBRoot   = "/usr/local/solovey-ui/db"
	hardenedDBRoot = "/var/lib/solovey-ui/db"
)

type Host struct {
	systemctl      string
	systemdAnalyze string
	now            func() time.Time
}

type checkpointV1 struct {
	Schema              int              `json:"schema"`
	OperationID         string           `json:"operationId"`
	FromProfile         domain.ProfileID `json:"fromProfile"`
	TargetProfile       domain.ProfileID `json:"targetProfile"`
	ExpectedPosture     string           `json:"expectedPosture"`
	OriginalUnit        []byte           `json:"originalUnit,omitempty"`
	OriginalUnitMode    uint32           `json:"originalUnitMode"`
	OriginalUnitUID     uint32           `json:"originalUnitUid"`
	OriginalUnitGID     uint32           `json:"originalUnitGid"`
	OriginalUnitTarget  string           `json:"originalUnitTarget,omitempty"`
	OriginalMarker      []byte           `json:"originalMarker,omitempty"`
	MarkerPresent       bool             `json:"markerPresent"`
	OriginalMarkerMode  uint32           `json:"originalMarkerMode"`
	OriginalMarkerUID   uint32           `json:"originalMarkerUid"`
	OriginalMarkerGID   uint32           `json:"originalMarkerGid"`
	HardenedRootPresent bool             `json:"hardenedRootPresent"`
	HardenedRootMode    uint32           `json:"hardenedRootMode"`
	HardenedRootUID     uint32           `json:"hardenedRootUid"`
	HardenedRootGID     uint32           `json:"hardenedRootGid"`
	CreatedAt           int64            `json:"createdAt"`
	Revision            string           `json:"revision"`
}

func RegisterHandlers(registry *broker.Registry) error {
	systemctl, err := fixedBinary("/usr/bin/systemctl", "/bin/systemctl")
	if err != nil {
		return err
	}
	systemdAnalyze, _ := fixedBinary("/usr/bin/systemd-analyze", "/bin/systemd-analyze")
	host := &Host{systemctl: systemctl, systemdAnalyze: systemdAnalyze, now: time.Now}
	definitions := []struct {
		verb     broker.Verb
		mutation bool
		handler  broker.Handler
	}{
		{broker.VerbDeploymentObserve, false, host.observeHandler},
		{broker.VerbDeploymentDoctor, false, host.doctorHandler},
		{broker.VerbDeploymentPrepare, true, host.prepareHandler},
		{broker.VerbDeploymentApply, true, host.applyHandler},
		{broker.VerbDeploymentVerify, true, host.verifyHandler},
		{broker.VerbDeploymentRollback, true, host.rollbackHandler},
	}
	for _, value := range definitions {
		if err := registry.Register(value.verb, broker.Definition{Role: broker.RolePanel, Mutation: value.mutation, Handler: value.handler}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) observeHandler(ctx context.Context, request broker.Request, _ broker.PeerIdentity) (any, error) {
	if err := decodeEmpty(request); err != nil {
		return nil, err
	}
	posture, _, err := h.observe(ctx)
	if err != nil {
		return nil, broker.Failure(broker.CodeCapability, "native deployment posture is unavailable")
	}
	return ObservationV1{Posture: posture, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) doctorHandler(ctx context.Context, request broker.Request, _ broker.PeerIdentity) (any, error) {
	if err := decodeEmpty(request); err != nil {
		return nil, err
	}
	posture, _, observeErr := h.observe(ctx)
	var observed *domain.Posture
	if observeErr == nil {
		observed = &posture
	}
	capabilities := nativeCapabilities(observed, h.time())
	report := domain.DoctorReport{Capabilities: capabilities, GeneratedAt: h.time().Unix()}
	if observeErr != nil {
		report.Findings = append(report.Findings, finding("posture_unavailable", domain.SeverityCritical, "deployment.doctor.postureUnavailable", "inspect the selected systemd unit and deployment marker"))
	} else {
		report.Posture = &posture
		profile, _ := domain.Lookup(posture.Profile)
		if posture.PanelRoot {
			report.Findings = append(report.Findings, finding("panel_runs_as_root", domain.SeverityWarning, "deployment.doctor.panelRoot", "migrate explicitly to native-hardened after reviewing the preview"))
		}
		for _, reason := range posture.Reasons {
			remediation := "restore the packaged unit, deployment marker, broker sockets, and profile-specific data ownership"
			if reason == "broker_unavailable" {
				remediation = "restore and start both broker socket units and the broker service"
			}
			report.Findings = append(report.Findings, finding(reason, domain.SeverityCritical, "deployment.doctor."+reason, remediation))
		}
		if profile.Support == domain.TierExperimental {
			report.Findings = append(report.Findings, finding("experimental_profile", domain.SeverityWarning, "deployment.doctor.experimentalProfile", "retain a tested provider-console recovery path"))
		}
	}
	report = domain.FinalizeDoctor(report)
	return DoctorResultV1{Report: report, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) prepareHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request PrepareRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !validTarget(request.TargetProfile) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "deployment target profile is invalid")
	}
	posture, _, err := h.observe(ctx)
	if err != nil || envelope.Expected.Provider != ProviderRevision || envelope.Expected.Configuration != posture.Revision {
		return nil, broker.Failure(broker.CodeRevision, "deployment posture changed before preparation")
	}
	if posture.Profile == request.TargetProfile || posture.Runtime != domain.RuntimeNative {
		return nil, broker.Failure(broker.CodeValidation, "deployment profile transition is not supported")
	}
	if err := validatePackagedProfile(request.TargetProfile); err != nil {
		return nil, broker.Failure(broker.CodeCapability, "packaged deployment profile is unavailable")
	}
	if support, _ := h.directiveCapability(ctx, profilePath(request.TargetProfile), ""); support != domain.Available {
		return nil, broker.Failure(broker.CodeCapability, "installed systemd cannot validate the target profile")
	}
	checkpoint, err := captureCheckpoint(envelope.OperationID, posture, request.TargetProfile, h.time())
	if err != nil {
		return nil, broker.Failure(broker.CodeInternal, "deployment rollback checkpoint could not be captured")
	}
	if err := writeCheckpoint(checkpoint); err != nil {
		return nil, broker.Failure(broker.CodeInternal, "deployment rollback checkpoint could not be persisted")
	}
	return PrepareResultV1{CheckpointRef: checkpoint.Revision, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) applyHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ApplyRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !validTarget(request.TargetProfile) || !digest(request.CheckpointRef) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "deployment apply request is invalid")
	}
	checkpoint, err := readCheckpoint(request.CheckpointRef)
	if err != nil || checkpoint.OperationID != envelope.OperationID || checkpoint.TargetProfile != request.TargetProfile {
		return nil, broker.Failure(broker.CodeFence, "deployment checkpoint identity differs")
	}
	posture, _, err := h.observe(ctx)
	if err != nil || posture.Revision != checkpoint.ExpectedPosture {
		return nil, broker.Failure(broker.CodeRevision, "deployment posture changed after preparation")
	}
	if err := checkpointAuthorityMatches(checkpoint); err != nil {
		return nil, broker.Failure(broker.CodeRevision, "deployment unit or marker changed after preparation")
	}
	if _, err := h.run(ctx, "stop", "solovey-ui.service"); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "panel service could not be stopped for migration")
	}
	rollback := func() error {
		_, rollbackErr := h.rollbackCheckpoint(context.Background(), checkpoint)
		return rollbackErr
	}
	if checkpoint.FromProfile == domain.NativeLegacyRoot {
		if err := migrateLegacyData(checkpoint); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return nil, broker.Failure(broker.CodeRecoveryRequired, "legacy data staging failed and exact rollback requires recovery")
			}
			return nil, broker.Failure(broker.CodeExecution, "legacy panel data could not be staged safely")
		}
	}
	if err := selectProfile(request.TargetProfile); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return nil, broker.Failure(broker.CodeRecoveryRequired, "profile selection failed and exact rollback requires recovery")
		}
		return nil, broker.Failure(broker.CodeExecution, "deployment profile could not be selected")
	}
	if _, err := h.run(ctx, "daemon-reload"); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return nil, broker.Failure(broker.CodeRecoveryRequired, "profile reload failed and exact rollback requires recovery")
		}
		return nil, broker.Failure(broker.CodeExecution, "systemd profile reload failed")
	}
	if _, err := h.run(ctx, "start", "solovey-ui.service"); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return nil, broker.Failure(broker.CodeRecoveryRequired, "target profile failed and exact rollback requires recovery")
		}
		return nil, broker.Failure(broker.CodeExecution, "target panel profile failed to start and was rolled back")
	}
	return ApplyResultV1{TargetProfile: request.TargetProfile, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) verifyHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request VerifyRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !validTarget(request.TargetProfile) || !digest(request.CheckpointRef) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "deployment verification request is invalid")
	}
	checkpoint, err := readCheckpoint(request.CheckpointRef)
	if err != nil || checkpoint.OperationID != envelope.OperationID || checkpoint.TargetProfile != request.TargetProfile {
		return nil, broker.Failure(broker.CodeFence, "deployment verification checkpoint differs")
	}
	posture, unit, err := h.observe(ctx)
	verified := err == nil && posture.Profile == request.TargetProfile && posture.Validate(h.time()) == nil &&
		bytes.Contains(unit, []byte("NoNewPrivileges=true")) && bytes.Contains(unit, []byte("ProtectSystem=strict"))
	return VerifyResultV1{Verified: verified, Posture: posture, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) rollbackHandler(ctx context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request RollbackRequestV1
	if err := broker.DecodePayload(envelope.Payload, &request); err != nil || !digest(request.CheckpointRef) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "deployment rollback request is invalid")
	}
	checkpoint, err := readCheckpoint(request.CheckpointRef)
	if err != nil || checkpoint.OperationID != envelope.OperationID || checkpoint.FromProfile != request.FromProfile {
		return nil, broker.Failure(broker.CodeFence, "deployment rollback checkpoint differs")
	}
	posture, rollbackErr := h.rollbackCheckpoint(ctx, checkpoint)
	if rollbackErr != nil {
		return nil, broker.Failure(broker.CodeRecoveryRequired, "deployment rollback could not restore and verify exact prior authority")
	}
	verified := posture.Profile == request.FromProfile && posture.Validate(h.time()) == nil
	return RollbackResultV1{Verified: verified, Posture: posture, ProviderRevision: ProviderRevision}, nil
}

func (h *Host) rollbackCheckpoint(ctx context.Context, checkpoint checkpointV1) (domain.Posture, error) {
	if checkpointAuthorityMatches(checkpoint) == nil {
		if err := restoreMigratedData(checkpoint); err != nil {
			return domain.Posture{}, err
		}
	} else {
		if err := transitionAuthorityMatches(checkpoint); err != nil {
			return domain.Posture{}, err
		}
		if _, err := h.run(ctx, "stop", "solovey-ui.service"); err != nil {
			return domain.Posture{}, fmt.Errorf("stop panel before deployment rollback: %w", err)
		}
		if err := restoreMigratedData(checkpoint); err != nil {
			return domain.Posture{}, err
		}
		if err := restoreCheckpoint(checkpoint); err != nil {
			return domain.Posture{}, err
		}
	}
	if _, err := h.run(ctx, "daemon-reload"); err != nil {
		return domain.Posture{}, err
	}
	if _, err := h.run(ctx, "start", "solovey-ui.service"); err != nil {
		return domain.Posture{}, err
	}
	posture, _, err := h.observe(ctx)
	if err != nil || posture.Profile != checkpoint.FromProfile || posture.Validate(h.time()) != nil || checkpointAuthorityMatches(checkpoint) != nil {
		return domain.Posture{}, errors.New("rolled-back deployment authority did not verify")
	}
	return posture, nil
}

func (h *Host) observe(ctx context.Context) (domain.Posture, []byte, error) {
	show, err := h.run(ctx, "show", "solovey-ui.service", "--property="+systemdObservationProperties)
	if err != nil || !bytes.Contains(show, []byte("LoadState=loaded")) || !bytes.Contains(show, []byte("ActiveState=active")) {
		return domain.Posture{}, nil, errors.New("panel systemd service is not active")
	}
	properties := parseProperties(show)
	profile := detectProfile(properties)
	definition, ok := domain.Lookup(profile)
	if !ok || definition.Runtime != domain.RuntimeNative {
		return domain.Posture{}, nil, errors.New("native deployment profile is unknown")
	}
	activeProfile, unit, activeErr := activeSystemdProfile(properties)
	expectedWritePaths, pathsOK := unitPathDirective(unit, "ReadWritePaths")
	if !pathsOK {
		expectedWritePaths = nil
	}
	reasons := systemdProfileReasons(profile, properties, expectedWritePaths)
	if activeErr != nil {
		reasons = append(reasons, "active_unit_identity_unavailable")
	} else if activeProfile != profile {
		reasons = append(reasons, "installed_active_profile_mismatch")
	}
	if markerProfile, markerOK := installedMarkerProfile(); !markerOK || markerProfile != profile {
		reasons = append(reasons, "deployment_marker_mismatch")
	}
	uid, gid, err := serviceIdentity(properties["User"], properties["Group"])
	if err != nil {
		return domain.Posture{}, nil, err
	}
	brokerAvailable := false
	brokerSocketRevision := domain.Revision("broker-sockets-unavailable")
	_, brokerGID, brokerGroupErr := serviceIdentity("root", "solovey-ui")
	if active, activeErr := h.run(ctx, "is-active", "solovey-privileged-broker.service"); activeErr == nil && strings.TrimSpace(string(active)) == "active" {
		brokerAvailable = brokerGroupErr == nil && secureSocket(broker.DefaultSocketPath, brokerGID) == nil && secureSocket(broker.ProofSocketPath, brokerGID) == nil
		if brokerAvailable {
			brokerSocketRevision, _ = secureSocketSetRevision(brokerGID, broker.DefaultSocketPath, broker.ProofSocketPath)
		}
	}
	if definition.BrokerRequired && !brokerAvailable {
		reasons = append(reasons, "broker_unavailable")
	}
	dataRevision, err := directoryRevision(selectedDBRoot(profile), uid, gid, !definition.PanelRoot)
	if err != nil {
		dataRevision = domain.Revision("unavailable")
		reasons = append(reasons, "data_directory_unsafe")
	} else if directory, secureErr := secureDirectory(selectedDBRoot(profile), uid, gid); secureErr != nil {
		reasons = append(reasons, "data_directory_unsafe")
	} else if !definition.PanelRoot && directory.Mode().Perm() != 0o700 {
		reasons = append(reasons, "data_directory_mode_mismatch")
	}
	if profile == domain.NativeNetworkAdvanced {
		reasons = append(reasons, "separate_network_runtime_unavailable")
	}
	versionOutput, versionErr := h.run(ctx, "--version")
	if versionErr != nil {
		versionOutput = nil
	}
	bootRevision := ""
	if bootID, bootErr := os.ReadFile("/proc/sys/kernel/random/boot_id"); bootErr == nil && len(bytes.TrimSpace(bootID)) >= 32 {
		bootRevision = domain.Revision(string(bytes.TrimSpace(bootID)))
	}
	directiveSupport, directiveRevision := h.directiveCapability(ctx, profilePath(domain.NativeHardened), string(versionOutput))
	executableRevision, executableErr := secureExecutableRevision("/usr/local/solovey-ui/releases/current/solovey-ui")
	if executableErr != nil {
		executableRevision = ""
	}
	reasons = uniqueCodes(reasons)
	now := h.time()
	fragment := filepath.Clean(properties["FragmentPath"])
	fragmentTrusted := activeErr == nil && (fragment == activeUnit || fragment == profilePath(activeProfile))
	fragmentRevision := ""
	if activeErr == nil {
		fragmentRevision = domain.Revision(string(unit))
	}
	systemdFacts, systemdReasons := projectSystemdActualState(systemdActualInput{Profile: profile, Properties: properties,
		VersionOutput: string(versionOutput), ManagerBootRevision: bootRevision, DirectiveSupport: directiveSupport,
		DirectiveCapabilityRevision: directiveRevision, FragmentTrusted: fragmentTrusted, FragmentRevision: fragmentRevision, ExecutableRevision: executableRevision,
		BrokerSocketRevision: brokerSocketRevision, ObservedAt: now})
	reasons = uniqueCodes(append(reasons, systemdReasons...))
	posture := domain.Posture{Schema: domain.SchemaV1, Profile: profile, InstalledProfile: profile, ActiveProfile: activeProfile, Runtime: domain.RuntimeNative,
		PanelUID: uid, PanelGID: gid, PanelRoot: uid == 0, BrokerAvailable: brokerAvailable,
		ServiceRevision: domain.Revision(string(show)), DataRevision: dataRevision, HardeningRevision: domain.Revision(string(unit)),
		Systemd: &systemdFacts, ObservedAt: now.Unix(), ExpiresAt: now.Add(2 * time.Minute).Unix(), Reasons: reasons}
	if brokerAvailable {
		posture.BrokerRevision = ProviderRevision
	}
	if len(reasons) == 0 {
		posture.VerifiedProfile = profile
	}
	domain.SetPostureRevision(&posture)
	return posture, unit, nil
}

func installedMarkerProfile() (domain.ProfileID, bool) {
	data, _, err := secureRootFile(profileMarker, 128)
	if err != nil {
		return "", false
	}
	profile := domain.ProfileID(strings.TrimSpace(string(data)))
	definition, ok := domain.Lookup(profile)
	return profile, ok && definition.Runtime == domain.RuntimeNative
}

func activeSystemdProfile(properties map[string]string) (domain.ProfileID, []byte, error) {
	info, err := os.Lstat(activeUnit)
	if err != nil {
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return "", nil, errors.New("active unit link is not root-owned")
		}
		target, err := os.Readlink(activeUnit)
		if err != nil || !filepath.IsAbs(target) {
			return "", nil, errors.New("active unit link target is invalid")
		}
		target = filepath.Clean(target)
		for _, profile := range []domain.ProfileID{domain.NativeHardened, domain.NativeNetworkAdvanced, domain.NativeLegacyRoot} {
			if target == profilePath(profile) {
				data, _, readErr := secureRootFile(target, 128<<10)
				return profile, data, readErr
			}
		}
		return "", nil, errors.New("active unit link target is not packaged")
	}
	if info.Mode().IsRegular() && oneOf(properties["User"], "", "root") {
		data, _, readErr := secureRootFile(activeUnit, 128<<10)
		return domain.NativeLegacyRoot, data, readErr
	}
	return "", nil, errors.New("active unit identity is unsupported")
}

func (h *Host) run(ctx context.Context, args ...string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, h.systemctl, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var buffer limitBuffer
	command.Stdout, command.Stderr = &buffer, &buffer
	err := command.Run()
	if buffer.truncated || bounded.Err() != nil {
		return buffer.data.Bytes(), errors.New("systemd operation exceeded its bound")
	}
	return buffer.data.Bytes(), err
}

func (h *Host) directiveCapability(ctx context.Context, unitPath, versionOutput string) (domain.Availability, string) {
	if h == nil || h.systemdAnalyze == "" || unitPath != activeUnit && !knownProfilePath(unitPath) {
		return domain.Unknown, domain.Revision("systemd-analyze-unavailable")
	}
	unit, _, unitErr := secureRootFile(unitPath, 128<<10)
	analyzerRevision, analyzerErr := secureExecutableRevision(h.systemdAnalyze)
	if unitErr != nil || analyzerErr != nil {
		return domain.Unavailable, domain.Revision(struct {
			Unit     string
			UnitSafe bool
			ToolSafe bool
		}{filepath.Base(unitPath), unitErr == nil, analyzerErr == nil})
	}
	if versionOutput == "" {
		output, err := h.run(ctx, "--version")
		if err != nil {
			return domain.Unknown, domain.Revision("systemd-version-unavailable")
		}
		versionOutput = string(output)
	}
	version, versionOK := systemdVersionNumber(versionOutput)
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	supported := false
	if versionOK && version >= 249 {
		command := exec.CommandContext(bounded, h.systemdAnalyze, "verify", "--man=no", "--recursive-errors=yes", unitPath)
		command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
		var buffer limitBuffer
		command.Stdout, command.Stderr = &buffer, &buffer
		err := command.Run()
		supported = err == nil && !buffer.truncated && bounded.Err() == nil
	}
	revision := domain.Revision(struct {
		Unit, UnitRevision, AnalyzerRevision, Version string
		Supported                                     bool
	}{filepath.Base(unitPath), domain.Revision(string(unit)), analyzerRevision, parseSystemdVersion(versionOutput), supported})
	if supported {
		return domain.Available, revision
	}
	return domain.Unavailable, revision
}

type limitBuffer struct {
	data      bytes.Buffer
	truncated bool
}

func (b *limitBuffer) Write(value []byte) (int, error) {
	length := len(value)
	remaining := 256<<10 - b.data.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || length > 0
		return length, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.data.Write(value)
	return length, nil
}

func decodeEmpty(request broker.Request) error {
	var empty EmptyV1
	if err := broker.DecodePayload(request.Payload, &empty); err != nil {
		return broker.Failure(broker.CodeInvalidRequest, "deployment broker payload is malformed")
	}
	return nil
}

func validTarget(profile domain.ProfileID) bool {
	return profile == domain.NativeHardened
}

func profilePath(profile domain.ProfileID) string {
	switch profile {
	case domain.NativeHardened:
		return filepath.Join(profileRoot, "solovey-ui-native-hardened.service")
	case domain.NativeNetworkAdvanced:
		return filepath.Join(profileRoot, "solovey-ui-native-network-advanced.service")
	case domain.NativeLegacyRoot:
		return filepath.Join(profileRoot, "solovey-ui-native-legacy-root.service")
	default:
		return ""
	}
}

func knownProfilePath(path string) bool {
	path = filepath.Clean(path)
	for _, profile := range []domain.ProfileID{domain.NativeHardened, domain.NativeNetworkAdvanced, domain.NativeLegacyRoot} {
		if path == profilePath(profile) {
			return true
		}
	}
	return false
}

func validatePackagedProfile(profile domain.ProfileID) error {
	path := profilePath(profile)
	if path == "" {
		return errors.New("profile is not packaged")
	}
	_, _, err := secureRootFile(path, 128<<10)
	return err
}

func detectProfile(properties map[string]string) domain.ProfileID {
	if data, _, err := secureRootFile(profileMarker, 128); err == nil {
		value := domain.ProfileID(strings.TrimSpace(string(data)))
		if profile, ok := domain.Lookup(value); ok && profile.Runtime == domain.RuntimeNative {
			return value
		}
	}
	if properties["User"] == "" || properties["User"] == "root" {
		return domain.NativeLegacyRoot
	}
	return domain.NativeHardened
}

func parseProperties(data []byte) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func serviceIdentity(userName, groupName string) (uint32, uint32, error) {
	if userName == "" {
		userName = "root"
	}
	if groupName == "" {
		groupName = userName
	}
	account, err := user.Lookup(userName)
	if err != nil {
		return 0, 0, err
	}
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, 0, err
	}
	uid, errA := strconv.ParseUint(account.Uid, 10, 32)
	gid, errB := strconv.ParseUint(group.Gid, 10, 32)
	if errA != nil || errB != nil {
		return 0, 0, errors.New("panel service numeric identity is malformed")
	}
	return uint32(uid), uint32(gid), nil
}

func selectedDBRoot(profile domain.ProfileID) string {
	if profile == domain.NativeLegacyRoot {
		return legacyDBRoot
	}
	return hardenedDBRoot
}

func secureDirectory(path string, expectedUID, expectedGID uint32) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("deployment data directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID || stat.Gid != expectedGID {
		return nil, errors.New("deployment data directory owner is unsafe")
	}
	return info, nil
}

func directoryRevision(path string, expectedUID, expectedGID uint32, strictMode bool) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("deployment data directory is unavailable")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 256 {
		return "", errors.New("deployment data directory cardinality is invalid")
	}
	type fact struct {
		Name        string
		Size, MTime int64
		Mode        uint32
	}
	facts := make([]fact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.Contains(entry.Name(), "/") {
			continue
		}
		item, err := entry.Info()
		if err != nil || !item.Mode().IsRegular() {
			return "", errors.New("deployment data entry is unsafe")
		}
		stat, ok := item.Sys().(*syscall.Stat_t)
		marker := strings.HasPrefix(entry.Name(), ".solovey-migration-")
		unsafeMode := item.Mode().Perm()&0o111 != 0 || strictMode && item.Mode().Perm()&0o077 != 0 || !strictMode && item.Mode().Perm()&0o022 != 0
		if !ok || marker && (stat.Uid != 0 || stat.Gid != 0 || item.Mode().Perm() != 0o400) ||
			!marker && (stat.Uid != expectedUID || stat.Gid != expectedGID || unsafeMode) {
			return "", errors.New("deployment data entry owner or mode is unsafe")
		}
		facts = append(facts, fact{entry.Name(), item.Size(), item.ModTime().UnixNano(), uint32(item.Mode().Perm())})
	}
	return domain.Revision(facts), nil
}

func secureSocket(path string, expectedGID uint32) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o660 {
		return errors.New("broker socket is unsafe")
	}
	stat, statOK := info.Sys().(*syscall.Stat_t)
	if !statOK || stat.Uid != 0 || stat.Gid != expectedGID || stat.Nlink != 1 {
		return errors.New("broker socket is not root-owned")
	}
	return nil
}

func secureSocketSetRevision(expectedGID uint32, paths ...string) (string, error) {
	type fact struct {
		Name          string
		UID, GID      uint32
		Mode          uint32
		Device, Inode uint64
	}
	facts := make([]fact, 0, len(paths))
	for _, path := range paths {
		if err := secureSocket(path, expectedGID); err != nil {
			return "", err
		}
		info, err := os.Lstat(path)
		stat, ok := itemSys(info)
		if err != nil || !ok {
			return "", errors.New("broker socket metadata is unavailable")
		}
		facts = append(facts, fact{filepath.Base(path), stat.Uid, stat.Gid, uint32(info.Mode().Perm()), uint64(stat.Dev), stat.Ino})
	}
	return domain.Revision(facts), nil
}

func secureExecutableRevision(path string) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Mode&0o022 != 0 || stat.Mode&0o111 == 0 || stat.Nlink != 1 || stat.Size <= 0 || stat.Size > 512<<20 {
		return "", errors.New("panel executable identity is unsafe")
	}
	return domain.Revision(struct {
		Name                      string
		Device, Inode             uint64
		Size, MTimeSec, MTimeNSec int64
		CTimeSec, CTimeNSec       int64
		UID, GID, Mode, LinkCount uint32
	}{filepath.Base(path), uint64(stat.Dev), stat.Ino, stat.Size, int64(stat.Mtim.Sec), int64(stat.Mtim.Nsec), int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec),
		stat.Uid, stat.Gid, stat.Mode, uint32(stat.Nlink)}), nil
}

func captureCheckpoint(operationID string, posture domain.Posture, target domain.ProfileID, now time.Time) (checkpointV1, error) {
	checkpoint := checkpointV1{Schema: 1, OperationID: operationID, FromProfile: posture.Profile,
		TargetProfile: target, ExpectedPosture: posture.Revision, CreatedAt: now.Unix()}
	info, err := os.Lstat(activeUnit)
	if err != nil {
		return checkpointV1{}, err
	}
	unitStat, unitStatOK := info.Sys().(*syscall.Stat_t)
	if !unitStatOK || unitStat.Uid != 0 {
		return checkpointV1{}, errors.New("active unit owner is unsafe")
	}
	checkpoint.OriginalUnitUID, checkpoint.OriginalUnitGID = unitStat.Uid, unitStat.Gid
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(activeUnit)
		if err != nil || !filepath.IsAbs(target) || !knownProfilePath(filepath.Clean(target)) {
			return checkpointV1{}, errors.New("active unit symlink is unsafe")
		}
		checkpoint.OriginalUnitTarget = filepath.Clean(target)
	} else if info.Mode().IsRegular() {
		data, _, err := secureRootFile(activeUnit, 128<<10)
		if err != nil {
			return checkpointV1{}, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return checkpointV1{}, errors.New("active unit metadata is unavailable")
		}
		checkpoint.OriginalUnit, checkpoint.OriginalUnitMode = data, uint32(info.Mode().Perm())
		checkpoint.OriginalUnitUID, checkpoint.OriginalUnitGID = stat.Uid, stat.Gid
	} else {
		return checkpointV1{}, errors.New("active unit is not a regular file or symlink")
	}
	if data, markerInfo, err := secureRootFile(profileMarker, 128); err == nil {
		checkpoint.MarkerPresent, checkpoint.OriginalMarker = true, data
		stat, ok := markerInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return checkpointV1{}, errors.New("deployment marker metadata is unavailable")
		}
		checkpoint.OriginalMarkerMode = uint32(markerInfo.Mode().Perm())
		checkpoint.OriginalMarkerUID, checkpoint.OriginalMarkerGID = stat.Uid, stat.Gid
	} else if _, statErr := os.Lstat(profileMarker); !errors.Is(statErr, os.ErrNotExist) {
		return checkpointV1{}, err
	}
	if err := captureHardenedRoot(&checkpoint); err != nil {
		return checkpointV1{}, err
	}
	checkpoint.Revision = checkpointRevision(checkpoint)
	return checkpoint, nil
}

func captureHardenedRoot(checkpoint *checkpointV1) error {
	if checkpoint == nil || checkpoint.FromProfile != domain.NativeLegacyRoot || checkpoint.TargetProfile != domain.NativeHardened {
		return nil
	}
	account, err := user.Lookup("solovey-ui")
	if err != nil {
		return err
	}
	group, err := user.LookupGroup("solovey-ui")
	if err != nil {
		return err
	}
	uid, errA := strconv.ParseUint(account.Uid, 10, 32)
	gid, errB := strconv.ParseUint(group.Gid, 10, 32)
	if errA != nil || errB != nil {
		return errors.New("solovey-ui numeric identity is malformed")
	}
	info, err := os.Lstat(hardenedDBRoot)
	if errors.Is(err, os.ErrNotExist) {
		parent, parentErr := secureDirectory(filepath.Dir(hardenedDBRoot), uint32(uid), uint32(gid))
		if parentErr != nil || parent.Mode().Perm()&0o027 != 0 {
			return errors.New("hardened data parent is unsafe")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if _, secureErr := secureDirectory(hardenedDBRoot, uint32(uid), uint32(gid)); secureErr != nil || info.Mode().Perm() != 0o700 {
		return errors.New("hardened data target mode or ownership is unsafe")
	}
	entries, err := os.ReadDir(hardenedDBRoot)
	if err != nil || len(entries) != 0 {
		return errors.New("hardened data target is not empty")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("hardened data metadata is unavailable")
	}
	checkpoint.HardenedRootPresent = true
	checkpoint.HardenedRootMode = uint32(info.Mode().Perm())
	checkpoint.HardenedRootUID, checkpoint.HardenedRootGID = stat.Uid, stat.Gid
	return nil
}

func writeCheckpoint(checkpoint checkpointV1) error {
	if checkpoint.Revision != checkpointRevision(checkpoint) {
		return errors.New("deployment checkpoint revision differs")
	}
	if err := ensureRootDirectory(checkpointRoot, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(checkpointRoot)
	if err != nil || len(entries) >= maxCheckpoints {
		return errors.New("deployment checkpoint retention bound requires operator recovery")
	}
	data, err := json.Marshal(checkpoint)
	if err != nil || len(data) > 256<<10 {
		return errors.New("deployment checkpoint is too large")
	}
	return atomicWrite(filepath.Join(checkpointRoot, checkpoint.Revision+".json"), data, 0o600, 0, 0)
}

func readCheckpoint(revision string) (checkpointV1, error) {
	if !digest(revision) {
		return checkpointV1{}, errors.New("deployment checkpoint reference is invalid")
	}
	data, _, err := secureRootFile(filepath.Join(checkpointRoot, revision+".json"), 256<<10)
	if err != nil {
		return checkpointV1{}, err
	}
	var checkpoint checkpointV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&checkpoint); err != nil || checkpoint.Schema != 1 || checkpoint.Revision != revision || checkpointRevision(checkpoint) != revision {
		return checkpointV1{}, errors.New("deployment checkpoint is malformed")
	}
	return checkpoint, nil
}

func checkpointRevision(checkpoint checkpointV1) string {
	checkpoint.Revision = ""
	data, _ := json.Marshal(checkpoint)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func selectProfile(profile domain.ProfileID) error {
	path := profilePath(profile)
	if err := validatePackagedProfile(profile); err != nil {
		return err
	}
	temporary := activeUnit + ".solovey-new"
	_ = os.Remove(temporary)
	if err := os.Symlink(path, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, activeUnit); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return atomicWrite(profileMarker, []byte(profile+"\n"), 0o644, 0, 0)
}

func restoreCheckpoint(checkpoint checkpointV1) error {
	if checkpoint.OriginalUnitTarget != "" {
		temporary := activeUnit + ".solovey-rollback"
		_ = os.Remove(temporary)
		if err := os.Symlink(checkpoint.OriginalUnitTarget, temporary); err != nil {
			return err
		}
		if err := os.Lchown(temporary, int(checkpoint.OriginalUnitUID), int(checkpoint.OriginalUnitGID)); err != nil {
			_ = os.Remove(temporary)
			return err
		}
		if err := os.Rename(temporary, activeUnit); err != nil {
			_ = os.Remove(temporary)
			return err
		}
	} else if len(checkpoint.OriginalUnit) != 0 {
		if err := atomicWrite(activeUnit, checkpoint.OriginalUnit, os.FileMode(checkpoint.OriginalUnitMode), int(checkpoint.OriginalUnitUID), int(checkpoint.OriginalUnitGID)); err != nil {
			return err
		}
	} else {
		return errors.New("deployment checkpoint has no original unit")
	}
	if checkpoint.MarkerPresent {
		return atomicWrite(profileMarker, checkpoint.OriginalMarker, os.FileMode(checkpoint.OriginalMarkerMode), int(checkpoint.OriginalMarkerUID), int(checkpoint.OriginalMarkerGID))
	}
	info, err := os.Lstat(profileMarker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("deployment marker rollback target is unsafe")
	}
	return os.Remove(profileMarker)
}

func checkpointAuthorityMatches(checkpoint checkpointV1) error {
	if err := checkpointUnitMatches(checkpoint); err != nil {
		return err
	}
	return checkpointMarkerMatches(checkpoint)
}

func checkpointUnitMatches(checkpoint checkpointV1) error {
	info, err := os.Lstat(activeUnit)
	if err != nil {
		return err
	}
	if checkpoint.OriginalUnitTarget != "" {
		if info.Mode()&os.ModeSymlink == 0 {
			return errors.New("active unit is no longer the checkpoint symlink")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		target, readErr := os.Readlink(activeUnit)
		if !ok || stat.Uid != checkpoint.OriginalUnitUID || stat.Gid != checkpoint.OriginalUnitGID || stat.Nlink != 1 || readErr != nil || filepath.Clean(target) != checkpoint.OriginalUnitTarget {
			return errors.New("active unit symlink differs from checkpoint")
		}
	} else {
		data, current, readErr := secureRootFile(activeUnit, 128<<10)
		if readErr != nil || !bytes.Equal(data, checkpoint.OriginalUnit) || uint32(current.Mode().Perm()) != checkpoint.OriginalUnitMode {
			return errors.New("active unit differs from checkpoint")
		}
		stat, ok := current.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != checkpoint.OriginalUnitUID || stat.Gid != checkpoint.OriginalUnitGID {
			return errors.New("active unit metadata differs from checkpoint")
		}
	}
	return nil
}

func checkpointMarkerMatches(checkpoint checkpointV1) error {
	marker, markerInfo, markerErr := secureRootFile(profileMarker, 128)
	if !checkpoint.MarkerPresent {
		if errors.Is(markerErr, os.ErrNotExist) {
			return nil
		}
		return errors.New("deployment marker appeared after checkpoint")
	}
	if markerErr != nil || !bytes.Equal(marker, checkpoint.OriginalMarker) || uint32(markerInfo.Mode().Perm()) != checkpoint.OriginalMarkerMode {
		return errors.New("deployment marker differs from checkpoint")
	}
	stat, ok := markerInfo.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != checkpoint.OriginalMarkerUID || stat.Gid != checkpoint.OriginalMarkerGID {
		return errors.New("deployment marker metadata differs from checkpoint")
	}
	return nil
}

func selectedUnitMatches(profile domain.ProfileID) error {
	path := profilePath(profile)
	if path == "" {
		return errors.New("selected profile is invalid")
	}
	info, err := os.Lstat(activeUnit)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return errors.New("selected unit is not an exact profile symlink")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	target, readErr := os.Readlink(activeUnit)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || readErr != nil || filepath.Clean(target) != path {
		return errors.New("selected unit identity differs")
	}
	return nil
}

func selectedMarkerMatches(profile domain.ProfileID) error {
	marker, markerInfo, markerErr := secureRootFile(profileMarker, 128)
	if markerErr != nil || strings.TrimSpace(string(marker)) != string(profile) || markerInfo.Mode().Perm() != 0o644 {
		return errors.New("selected deployment marker differs")
	}
	markerStat, markerOK := markerInfo.Sys().(*syscall.Stat_t)
	if !markerOK || markerStat.Uid != 0 || markerStat.Gid != 0 {
		return errors.New("selected deployment marker owner differs")
	}
	return nil
}

func transitionAuthorityMatches(checkpoint checkpointV1) error {
	unit, marker := transitionForeign, transitionForeign
	if checkpointUnitMatches(checkpoint) == nil {
		unit = transitionOriginal
	} else if selectedUnitMatches(checkpoint.TargetProfile) == nil {
		unit = transitionTarget
	}
	if checkpointMarkerMatches(checkpoint) == nil {
		marker = transitionOriginal
	} else if selectedMarkerMatches(checkpoint.TargetProfile) == nil {
		marker = transitionTarget
	}
	if !transitionAuthoritySafe(unit, marker) {
		return errors.New("foreign newer deployment authority prevents rollback")
	}
	return nil
}

func migrateLegacyData(checkpoint checkpointV1) error {
	account, err := user.Lookup("solovey-ui")
	if err != nil {
		return err
	}
	group, err := user.LookupGroup("solovey-ui")
	if err != nil {
		return err
	}
	uid64, errA := strconv.ParseUint(account.Uid, 10, 32)
	gid64, errB := strconv.ParseUint(group.Gid, 10, 32)
	if errA != nil || errB != nil {
		return errors.New("solovey-ui numeric identity is malformed")
	}
	uid, gid := int(uid64), int(gid64)
	if checkpoint.HardenedRootPresent {
		info, secureErr := secureDirectory(hardenedDBRoot, uint32(uid), uint32(gid))
		if secureErr != nil || uint32(info.Mode().Perm()) != checkpoint.HardenedRootMode {
			return errors.New("hardened database root changed after checkpoint")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != checkpoint.HardenedRootUID || stat.Gid != checkpoint.HardenedRootGID {
			return errors.New("hardened database root metadata changed after checkpoint")
		}
	} else {
		if _, statErr := os.Lstat(hardenedDBRoot); !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("hardened database root appeared after checkpoint")
		}
		if err := os.Mkdir(hardenedDBRoot, 0o700); err != nil {
			return err
		}
		if err := os.Chown(hardenedDBRoot, uid, gid); err != nil {
			_ = os.Remove(hardenedDBRoot)
			return err
		}
	}
	targetEntries, err := os.ReadDir(hardenedDBRoot)
	if err != nil || len(targetEntries) != 0 {
		return errors.New("hardened database root is not empty")
	}
	entries, err := os.ReadDir(legacyDBRoot)
	if err != nil || len(entries) > 64 {
		return errors.New("legacy database directory is unavailable")
	}
	available := make(map[string]os.DirEntry, len(entries))
	for _, entry := range entries {
		available[entry.Name()] = entry
	}
	if main, ok := available["solovey-ui.db"]; !ok || main.IsDir() {
		return errors.New("legacy primary database is unavailable")
	}
	for _, name := range []string{"solovey-ui.db", "solovey-ui.db-wal", "solovey-ui.db-shm"} {
		entry, ok := available[name]
		if !ok {
			continue
		}
		if entry.IsDir() {
			return errors.New("legacy database entry is unsafe")
		}
		if err := copyRegular(filepath.Join(legacyDBRoot, name), filepath.Join(hardenedDBRoot, name), 0o600, uid, gid); err != nil {
			return err
		}
	}
	marker := filepath.Join(hardenedDBRoot, ".solovey-migration-"+checkpoint.Revision)
	if err := atomicWrite(marker, []byte(checkpoint.Revision+"\n"), 0o400, 0, 0); err != nil {
		return err
	}
	return syncDir(hardenedDBRoot)
}

func restoreMigratedData(checkpoint checkpointV1) error {
	if checkpoint.FromProfile != domain.NativeLegacyRoot || checkpoint.TargetProfile != domain.NativeHardened {
		return nil
	}
	account, err := user.Lookup("solovey-ui")
	if err != nil {
		return err
	}
	group, err := user.LookupGroup("solovey-ui")
	if err != nil {
		return err
	}
	uid64, errA := strconv.ParseUint(account.Uid, 10, 32)
	gid64, errB := strconv.ParseUint(group.Gid, 10, 32)
	if errA != nil || errB != nil {
		return errors.New("solovey-ui numeric identity is malformed")
	}
	_, err = os.Lstat(hardenedDBRoot)
	if errors.Is(err, os.ErrNotExist) && !checkpoint.HardenedRootPresent {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := secureDirectory(hardenedDBRoot, uint32(uid64), uint32(gid64)); err != nil {
		return err
	}
	entries, err := os.ReadDir(hardenedDBRoot)
	if err != nil {
		return err
	}
	markerName := ".solovey-migration-" + checkpoint.Revision
	facts := make([]stagedDataFact, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(hardenedDBRoot, entry.Name())
		item, statErr := os.Lstat(path)
		stat, statOK := itemSys(item)
		fact := stagedDataFact{Name: entry.Name(), Regular: statErr == nil && item != nil && item.Mode().IsRegular() && item.Mode()&os.ModeSymlink == 0 && statOK}
		if statOK {
			fact.UID, fact.GID = stat.Uid, stat.Gid
		}
		if item != nil {
			fact.Mode = uint32(item.Mode().Perm())
		}
		if entry.Name() == markerName {
			data, _, markerErr := secureRootFile(path, 128)
			fact.MarkerContentMatches = markerErr == nil && string(data) == checkpoint.Revision+"\n"
		}
		facts = append(facts, fact)
	}
	if err := validateStagedDataFacts(facts, markerName, uint32(uid64), uint32(gid64), checkpointAuthorityMatches(checkpoint) != nil); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(hardenedDBRoot, entry.Name())); err != nil {
			return err
		}
	}
	if checkpoint.HardenedRootPresent {
		if err := os.Chown(hardenedDBRoot, int(checkpoint.HardenedRootUID), int(checkpoint.HardenedRootGID)); err != nil {
			return err
		}
		if err := os.Chmod(hardenedDBRoot, os.FileMode(checkpoint.HardenedRootMode)); err != nil {
			return err
		}
		return syncDir(hardenedDBRoot)
	}
	if err := os.Remove(hardenedDBRoot); err != nil {
		return err
	}
	return syncDir(filepath.Dir(hardenedDBRoot))
}

func itemSys(info os.FileInfo) (*syscall.Stat_t, bool) {
	if info == nil {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat, ok
}

func copyRegular(source, target string, mode os.FileMode, uid, gid int) error {
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	input := os.NewFile(uintptr(fd), source)
	if input == nil {
		_ = unix.Close(fd)
		return errors.New("legacy database descriptor is unavailable")
	}
	defer input.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Size < 0 || before.Size > 4<<30 {
		return errors.New("legacy database file is unsafe")
	}
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".solovey-db-migration-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	cleanupOK := false
	defer func() {
		_ = temporary.Close()
		if !cleanupOK {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Chown(uid, gid); err != nil {
		return err
	}
	written, err := io.Copy(temporary, io.LimitReader(input, before.Size+1))
	var after unix.Stat_t
	statErr := unix.Fstat(fd, &after)
	if err != nil || written != before.Size || statErr != nil || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return errors.New("legacy database changed while copying")
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, target); err != nil {
		return err
	}
	cleanupOK = true
	return syncDir(directory)
}

func secureRootFile(path string, limit int64) ([]byte, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, errors.New("deployment file descriptor is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > limit || info.Mode().Perm()&0o022 != 0 {
		return nil, nil, errors.New("root-owned deployment file is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return nil, nil, errors.New("deployment file is not root-owned")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	var after unix.Stat_t
	statErr := unix.Fstat(fd, &after)
	if err != nil || statErr != nil || int64(len(data)) != info.Size() || after.Size != info.Size() || after.Uid != 0 {
		return nil, nil, errors.New("deployment file changed while reading")
	}
	return data, info, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode, uid, gid int) error {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("deployment write directory is unsafe")
	}
	temporary, err := os.CreateTemp(directory, ".solovey-deployment-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Chown(uid, gid); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return syncDir(directory)
}

func ensureRootDirectory(path string, mode os.FileMode) error {
	if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode.Perm() {
		return errors.New("deployment root directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return errors.New("deployment root directory is not root-owned")
	}
	return nil
}

func syncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func fixedBinary(paths ...string) (string, error) {
	for _, path := range paths {
		info, err := os.Lstat(path)
		stat, ok := itemSys(info)
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o022 == 0 && ok && stat.Uid == 0 {
			return path, nil
		}
	}
	return "", errors.New("fixed root-owned executable is unavailable")
}

func digest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func finding(code string, severity domain.Severity, message, remediation string) domain.Finding {
	return domain.Finding{Code: code, Severity: severity, MessageKey: message, Remediation: remediation}
}

func (h *Host) time() time.Time {
	if h.now != nil {
		return h.now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

var _ io.Writer = (*limitBuffer)(nil)
