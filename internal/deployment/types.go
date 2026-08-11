// Package deployment defines deployment profiles, doctor facts, and the
// durable migration state machine without importing HTTP, systemd, Docker, or
// filesystem implementations.
package deployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	SchemaV1       = "solovey-ui/deployment/v1"
	DoctorSchemaV1 = "solovey-ui/deployment-doctor/v1"
	ProviderV1     = "solovey-privileged-broker/deployment/v1"
)

type ProfileID string

const (
	NativeHardened        ProfileID = "native-hardened"
	NativeNetworkAdvanced ProfileID = "native-network-advanced"
	NativeLegacyRoot      ProfileID = "native-legacy-root"
	DockerHost            ProfileID = "docker-host-unprivileged"
	DockerBridge          ProfileID = "docker-bridge-explicit"
	DockerNetworkAdvanced ProfileID = "docker-network-advanced"
)

type Runtime string

const (
	RuntimeNative Runtime = "native"
	RuntimeDocker Runtime = "docker"
)

type SupportTier string

const (
	TierRecommended   SupportTier = "recommended"
	TierSupported     SupportTier = "supported"
	TierExperimental  SupportTier = "experimental"
	TierCompatibility SupportTier = "compatibility"
)

type Profile struct {
	ID                  ProfileID   `json:"id"`
	Runtime             Runtime     `json:"runtime"`
	Support             SupportTier `json:"support"`
	FreshInstallDefault bool        `json:"freshInstallDefault"`
	PanelRoot           bool        `json:"panelRoot"`
	BrokerRequired      bool        `json:"brokerRequired"`
	HostNetwork         bool        `json:"hostNetwork"`
	ExplicitPorts       bool        `json:"explicitPorts"`
	NetworkCapabilities []string    `json:"networkCapabilities,omitempty"`
	ProcessIdentities   []string    `json:"processIdentities"`
	WriteScopes         []string    `json:"writeScopes"`
	ServiceUnits        []string    `json:"serviceUnits"`
	EvidenceStatus      string      `json:"evidenceStatus"`
	Constraints         []string    `json:"constraints,omitempty"`
	Revision            string      `json:"revision"`
}

func Catalog() []Profile {
	profiles := []Profile{
		{ID: NativeHardened, Runtime: RuntimeNative, Support: TierRecommended, FreshInstallDefault: true, BrokerRequired: true,
			ProcessIdentities: []string{"panel-service-account", "privileged-broker-root"}, WriteScopes: []string{"panel-state", "panel-runtime", "panel-logs"},
			ServiceUnits: []string{"solovey-ui.service", "solovey-privileged-broker.service"}, EvidenceStatus: "NORMAL_CI_VERIFIED_LIVE_NOT_RUN",
			Constraints: []string{"high_ports_only", "no_tun", "no_transparent_proxy"}},
		{ID: NativeNetworkAdvanced, Runtime: RuntimeNative, Support: TierExperimental, BrokerRequired: true,
			NetworkCapabilities: []string{"CAP_NET_ADMIN", "CAP_NET_BIND_SERVICE", "CAP_NET_RAW"},
			ProcessIdentities:   []string{"panel-service-account", "privileged-broker-root"}, WriteScopes: []string{"panel-state", "panel-runtime"},
			ServiceUnits: []string{"solovey-ui.service", "solovey-privileged-broker.service"}, EvidenceStatus: "GENERATED_UNSUPPORTED_LIVE_NOT_RUN",
			Constraints: []string{"explicit_operator_opt_in", "tun_device", "separate_core_runtime_required"}},
		{ID: NativeLegacyRoot, Runtime: RuntimeNative, Support: TierCompatibility, PanelRoot: true, BrokerRequired: true,
			ProcessIdentities: []string{"legacy-panel-root", "privileged-broker-root"}, WriteScopes: []string{"legacy-install-root"},
			ServiceUnits: []string{"solovey-ui.service", "solovey-privileged-broker.service"}, EvidenceStatus: "COMPATIBILITY_LIVE_NOT_RUN",
			Constraints: []string{"existing_install_compatibility_only", "not_a_fresh_install_default"}},
		{ID: DockerHost, Runtime: RuntimeDocker, Support: TierRecommended, FreshInstallDefault: true, HostNetwork: true,
			ProcessIdentities: []string{"container-nonroot-65532"}, WriteScopes: []string{"bound-data", "bound-cert", "tmpfs-runtime"},
			ServiceUnits: []string{}, EvidenceStatus: "GENERATED_NORMAL_CI_VERIFIED_LIVE_NOT_RUN", Constraints: []string{"broker_unavailable", "host_firewall_unmanaged"}},
		{ID: DockerBridge, Runtime: RuntimeDocker, Support: TierSupported, ExplicitPorts: true,
			ProcessIdentities: []string{"container-nonroot-65532"}, WriteScopes: []string{"bound-data", "bound-cert", "tmpfs-runtime"},
			ServiceUnits: []string{}, EvidenceStatus: "GENERATED_NORMAL_CI_VERIFIED_LIVE_NOT_RUN", Constraints: []string{"explicit_tcp_udp_ports", "broker_unavailable", "host_firewall_unmanaged"}},
		{ID: DockerNetworkAdvanced, Runtime: RuntimeDocker, Support: TierExperimental, HostNetwork: true,
			NetworkCapabilities: []string{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"}, ProcessIdentities: []string{"container-nonroot-65532"},
			WriteScopes: []string{"bound-data", "bound-cert", "tmpfs-runtime", "tun-device"}, ServiceUnits: []string{},
			EvidenceStatus: "GENERATED_EXPERIMENTAL_LIVE_NOT_RUN", Constraints: []string{"explicit_operator_opt_in", "tun_device", "broker_unavailable"}},
	}
	for index := range profiles {
		copy := profiles[index]
		copy.Revision = ""
		profiles[index].Revision = Revision(copy)
	}
	return profiles
}

func Lookup(id ProfileID) (Profile, bool) {
	for _, profile := range Catalog() {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

type Availability string

const (
	Available   Availability = "AVAILABLE"
	Unavailable Availability = "UNAVAILABLE"
	Unknown     Availability = "UNKNOWN"
)

type Capabilities struct {
	Observe  Availability `json:"observe"`
	Doctor   Availability `json:"doctor"`
	Migrate  Availability `json:"migrate"`
	Rollback Availability `json:"rollback"`
	Reasons  []string     `json:"reasons,omitempty"`
	Revision string       `json:"revision"`
}

// SystemdActualState is the bounded, safe projection of the installed manager
// and selected panel unit. Raw environment, command lines, unit text, and
// arbitrary host paths are deliberately excluded.
type SystemdActualState struct {
	Schema                      string       `json:"schema"`
	Version                     string       `json:"version"`
	ManagerBootRevision         string       `json:"managerBootRevision"`
	DirectiveSupport            Availability `json:"directiveSupport"`
	DirectiveCapabilityRevision string       `json:"directiveCapabilityRevision"`
	Unit                        string       `json:"unit"`
	FragmentRevision            string       `json:"fragmentRevision"`
	DropInRevision              string       `json:"dropInRevision"`
	UnitFileState               string       `json:"unitFileState"`
	LoadState                   string       `json:"loadState"`
	ActiveState                 string       `json:"activeState"`
	SubState                    string       `json:"subState"`
	DaemonReloadRequired        bool         `json:"daemonReloadRequired"`
	User                        string       `json:"user"`
	Group                       string       `json:"group"`
	BoundingCapabilities        []string     `json:"boundingCapabilities"`
	AmbientCapabilities         []string     `json:"ambientCapabilities"`
	NoNewPrivileges             bool         `json:"noNewPrivileges"`
	SandboxRevision             string       `json:"sandboxRevision"`
	WritePaths                  []string     `json:"writePaths"`
	ReadOnlyPaths               []string     `json:"readOnlyPaths"`
	ExecutableRevision          string       `json:"executableRevision"`
	RuntimeDirectoryRevision    string       `json:"runtimeDirectoryRevision"`
	ResourceRevision            string       `json:"resourceRevision"`
	Restart                     string       `json:"restart"`
	RestartUSec                 string       `json:"restartUSec"`
	WatchdogUSec                string       `json:"watchdogUSec"`
	BrokerSocketRevision        string       `json:"brokerSocketRevision"`
	ObservedAt                  int64        `json:"observedAt"`
	ExpiresAt                   int64        `json:"expiresAt"`
	Revision                    string       `json:"revision"`
}

func (s SystemdActualState) Validate(now time.Time) error {
	copy := s
	copy.Revision = ""
	if s.Schema != SchemaV1 || !safeCode(s.Version, 32) || !safeCode(s.Unit, 64) ||
		!safeCode(s.UnitFileState, 32) || !safeCode(s.LoadState, 32) || !safeCode(s.ActiveState, 32) || !safeCode(s.SubState, 32) ||
		!safeCode(s.User, 64) || !safeCode(s.Group, 64) || !safeCode(s.Restart, 32) || !safeCode(s.RestartUSec, 32) || !safeCode(s.WatchdogUSec, 32) ||
		!digest(s.ManagerBootRevision) || !digest(s.DirectiveCapabilityRevision) || !digest(s.FragmentRevision) || !digest(s.DropInRevision) ||
		!digest(s.SandboxRevision) || !digest(s.ExecutableRevision) || !digest(s.RuntimeDirectoryRevision) || !digest(s.ResourceRevision) ||
		!digest(s.BrokerSocketRevision) || !digest(s.Revision) || s.Revision != Revision(copy) ||
		s.ObservedAt <= 0 || s.ExpiresAt <= now.Unix() || s.ExpiresAt > s.ObservedAt+300 ||
		(s.DirectiveSupport != Available && s.DirectiveSupport != Unavailable && s.DirectiveSupport != Unknown) {
		return errors.New("systemd actual-state projection is malformed or stale")
	}
	if !boundedCodes(s.BoundingCapabilities, 64) || !boundedCodes(s.AmbientCapabilities, 64) ||
		!boundedPaths(s.WritePaths) || !boundedPaths(s.ReadOnlyPaths) {
		return errors.New("systemd actual-state values are malformed")
	}
	return nil
}

func (c Capabilities) Validate() error {
	for _, value := range []Availability{c.Observe, c.Doctor, c.Migrate, c.Rollback} {
		if value != Available && value != Unavailable && value != Unknown {
			return errors.New("deployment capability availability is invalid")
		}
	}
	if len(c.Reasons) > 32 || !digest(c.Revision) || c.Revision != Revision(struct {
		Observe  Availability `json:"observe"`
		Doctor   Availability `json:"doctor"`
		Migrate  Availability `json:"migrate"`
		Rollback Availability `json:"rollback"`
		Reasons  []string     `json:"reasons,omitempty"`
		Revision string       `json:"revision"`
	}{c.Observe, c.Doctor, c.Migrate, c.Rollback, c.Reasons, ""}) {
		return errors.New("deployment capabilities are malformed")
	}
	for _, reason := range c.Reasons {
		if !safeCode(reason, 96) {
			return errors.New("deployment capability reason is malformed")
		}
	}
	return nil
}

type Posture struct {
	Schema            string              `json:"schema"`
	Profile           ProfileID           `json:"profile"`
	InstalledProfile  ProfileID           `json:"installedProfile"`
	ActiveProfile     ProfileID           `json:"activeProfile"`
	VerifiedProfile   ProfileID           `json:"verifiedProfile,omitempty"`
	Runtime           Runtime             `json:"runtime"`
	PanelUID          uint32              `json:"panelUid"`
	PanelGID          uint32              `json:"panelGid"`
	PanelRoot         bool                `json:"panelRoot"`
	BrokerAvailable   bool                `json:"brokerAvailable"`
	BrokerRevision    string              `json:"brokerRevision,omitempty"`
	ServiceRevision   string              `json:"serviceRevision"`
	DataRevision      string              `json:"dataRevision"`
	HardeningRevision string              `json:"hardeningRevision"`
	Systemd           *SystemdActualState `json:"systemd,omitempty"`
	ObservedAt        int64               `json:"observedAt"`
	ExpiresAt         int64               `json:"expiresAt"`
	Revision          string              `json:"revision"`
	Reasons           []string            `json:"reasons,omitempty"`
}

func (p Posture) Validate(now time.Time) error {
	if err := p.ValidateProjection(now); err != nil {
		return err
	}
	profile, _ := Lookup(p.Profile)
	if p.InstalledProfile != p.Profile || p.ActiveProfile != p.Profile || p.VerifiedProfile != p.Profile ||
		p.PanelRoot != profile.PanelRoot || profile.BrokerRequired && !p.BrokerAvailable || len(p.Reasons) != 0 {
		return errors.New("deployment posture is ambiguous")
	}
	return nil
}

// ValidateProjection accepts a bounded, honest partial observation. Installed,
// active, and verified are independent facts; the empty value means that the
// provider could not prove that stage. Validate remains the stricter contract
// for migration commit and rollback authority.
func (p Posture) ValidateProjection(now time.Time) error {
	profile, ok := Lookup(p.Profile)
	if !ok || p.Schema != SchemaV1 || p.Runtime != profile.Runtime ||
		p.ObservedAt <= 0 || p.ExpiresAt <= now.Unix() || p.ExpiresAt > p.ObservedAt+300 ||
		!digest(p.ServiceRevision) || !digest(p.DataRevision) || !digest(p.HardeningRevision) || !digest(p.Revision) || p.Revision != postureRevision(p) || len(p.Reasons) > 32 {
		return errors.New("deployment posture is invalid or stale")
	}
	for _, projected := range []ProfileID{p.InstalledProfile, p.ActiveProfile, p.VerifiedProfile} {
		if projected == "" {
			continue
		}
		candidate, exists := Lookup(projected)
		if !exists || candidate.Runtime != p.Runtime {
			return errors.New("deployment posture projection is invalid")
		}
	}
	if p.Runtime == RuntimeNative {
		if p.Systemd == nil || p.Systemd.Validate(now) != nil || p.Systemd.ObservedAt != p.ObservedAt || p.Systemd.ExpiresAt != p.ExpiresAt {
			return errors.New("native systemd actual-state projection is unavailable")
		}
		expectedIdentity := "solovey-ui"
		if profile.PanelRoot {
			expectedIdentity = "root"
		}
		if p.Systemd.Unit != "solovey-ui.service" || p.Systemd.User != expectedIdentity || p.Systemd.Group != expectedIdentity ||
			p.Systemd.NoNewPrivileges == profile.PanelRoot || p.PanelRoot != (p.PanelUID == 0) || p.PanelRoot != (p.PanelGID == 0) {
			return errors.New("native systemd profile facts are inconsistent")
		}
	} else if p.Systemd != nil {
		return errors.New("container posture cannot claim native systemd facts")
	}
	if p.VerifiedProfile != "" && p.VerifiedProfile != p.ActiveProfile || p.ActiveProfile != "" && p.InstalledProfile == "" {
		return errors.New("deployment posture projection order is invalid")
	}
	for _, reason := range p.Reasons {
		if !safeCode(reason, 96) {
			return errors.New("deployment posture reason is malformed")
		}
	}
	return nil
}

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
)

type Finding struct {
	Code        string   `json:"code"`
	Severity    Severity `json:"severity"`
	MessageKey  string   `json:"messageKey"`
	Remediation string   `json:"remediation"`
}

type DoctorReport struct {
	Schema       string       `json:"schema"`
	Posture      *Posture     `json:"posture,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	Profiles     []Profile    `json:"profiles"`
	Findings     []Finding    `json:"findings"`
	Healthy      bool         `json:"healthy"`
	State        string       `json:"state"`
	Desired      ProfileID    `json:"desiredProfile,omitempty"`
	Generated    ProfileID    `json:"generatedProfile,omitempty"`
	Installed    ProfileID    `json:"installedProfile,omitempty"`
	Active       ProfileID    `json:"activeProfile,omitempty"`
	Verified     ProfileID    `json:"verifiedProfile,omitempty"`
	Evidence     string       `json:"evidenceStatus"`
	GeneratedAt  int64        `json:"generatedAt"`
	Revision     string       `json:"revision"`
}

func FinalizeDoctor(report DoctorReport) DoctorReport {
	report.Schema = DoctorSchemaV1
	report.Profiles = Catalog()
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity != report.Findings[j].Severity {
			return report.Findings[i].Severity > report.Findings[j].Severity
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	report.Healthy = true
	report.State = "READY"
	report.Evidence = "NORMAL_CI_VERIFIED_LIVE_NOT_RUN"
	for _, finding := range report.Findings {
		if finding.Severity == SeverityCritical {
			report.Healthy = false
		}
	}
	if report.Posture == nil {
		report.State = "UNAVAILABLE"
	} else {
		report.Installed, report.Active, report.Verified = report.Posture.InstalledProfile, report.Posture.ActiveProfile, report.Posture.VerifiedProfile
		if report.Posture.Profile == NativeLegacyRoot {
			report.State = "LEGACY_COMPATIBILITY"
		}
		if !report.Healthy {
			report.State = "DEGRADED"
		}
	}
	report.Revision = ""
	report.Revision = Revision(report)
	return report
}

func (report DoctorReport) Validate(now time.Time) error {
	copy := report
	copy.Revision = ""
	if report.Schema != DoctorSchemaV1 || report.GeneratedAt <= 0 || report.GeneratedAt > now.Add(time.Minute).Unix() ||
		!digest(report.Revision) || report.Revision != Revision(copy) || report.Capabilities.Validate() != nil ||
		len(report.Profiles) != len(Catalog()) || len(report.Findings) > 64 || !safeCode(report.State, 64) || !safeCode(report.Evidence, 96) {
		return errors.New("deployment doctor report is malformed")
	}
	if report.Posture != nil && report.Posture.ObservedAt > report.GeneratedAt+1 {
		return errors.New("deployment doctor posture chronology is invalid")
	}
	if report.Posture != nil && report.Posture.ValidateProjection(now) != nil {
		return errors.New("deployment doctor posture projection is invalid")
	}
	for _, finding := range report.Findings {
		if !safeCode(finding.Code, 96) || len(finding.MessageKey) > 160 || len(finding.Remediation) > 512 ||
			finding.Severity != SeverityInfo && finding.Severity != SeverityWarning && finding.Severity != SeverityCritical {
			return errors.New("deployment doctor finding is malformed")
		}
	}
	return nil
}

type OperationState string

const (
	StateDraft                  OperationState = "DRAFT"
	StatePreflighted            OperationState = "PREFLIGHTED"
	StateApplying               OperationState = "APPLYING"
	StateVerifying              OperationState = "VERIFYING"
	StateCommitted              OperationState = "COMMITTED"
	StateRollbackPending        OperationState = "ROLLBACK_PENDING"
	StateRolledBack             OperationState = "ROLLED_BACK"
	StateManualRecoveryRequired OperationState = "MANUAL_RECOVERY_REQUIRED"
)

func (s OperationState) Terminal() bool {
	return s == StateCommitted || s == StateRolledBack || s == StateManualRecoveryRequired
}

type Operation struct {
	Schema             string         `json:"schema"`
	OperationID        string         `json:"operationId"`
	IdempotencyKey     string         `json:"idempotencyKey"`
	State              OperationState `json:"state"`
	FromProfile        ProfileID      `json:"fromProfile"`
	TargetProfile      ProfileID      `json:"targetProfile"`
	ExpectedPosture    string         `json:"expectedPosture"`
	ExpectedManagement string         `json:"expectedManagement"`
	CheckpointRef      string         `json:"checkpointRef,omitempty"`
	BrokerReceipt      string         `json:"brokerReceipt,omitempty"`
	Revision           uint64         `json:"revision"`
	RestoredUntrusted  bool           `json:"restoredUntrusted"`
	ReconciledAt       int64          `json:"reconciledAt,omitempty"`
	CreatedAt          int64          `json:"createdAt"`
	UpdatedAt          int64          `json:"updatedAt"`
	Reasons            []string       `json:"reasons,omitempty"`
	BindingRevision    string         `json:"bindingRevision"`
}

func (o Operation) Validate() error {
	from, fromOK := Lookup(o.FromProfile)
	target, targetOK := Lookup(o.TargetProfile)
	if o.Schema != SchemaV1 || !fromOK || !targetOK || from.Runtime != RuntimeNative || target.Runtime != RuntimeNative || o.FromProfile == o.TargetProfile {
		return errors.New("deployment operation profile identity is invalid")
	}
	if !validOperationState(o.State) || !safeID(o.OperationID, 96) || !safeID(o.IdempotencyKey, 96) || o.Revision == 0 {
		return errors.New("deployment operation authority is invalid")
	}
	if o.CreatedAt <= 0 || o.UpdatedAt < o.CreatedAt || o.ReconciledAt < 0 || o.ReconciledAt > o.UpdatedAt || len(o.Reasons) > 32 {
		return errors.New("deployment operation chronology is invalid")
	}
	if !digest(o.ExpectedPosture) || !digest(o.ExpectedManagement) || o.CheckpointRef != "" && !digest(o.CheckpointRef) || o.BrokerReceipt != "" && !digest(o.BrokerReceipt) {
		return errors.New("deployment operation revision is malformed")
	}
	if !digest(o.BindingRevision) || o.BindingRevision != OperationBinding(o) {
		return errors.New("deployment operation binding differs")
	}
	for _, reason := range o.Reasons {
		if !safeCode(reason, 96) {
			return errors.New("deployment operation reason is malformed")
		}
	}
	return nil
}

func validOperationState(state OperationState) bool {
	switch state {
	case StateDraft, StatePreflighted, StateApplying, StateVerifying, StateCommitted, StateRollbackPending, StateRolledBack, StateManualRecoveryRequired:
		return true
	default:
		return false
	}
}

func OperationBinding(operation Operation) string {
	copy := operation
	copy.BindingRevision = ""
	copy.State = ""
	copy.Revision = 0
	copy.CheckpointRef = ""
	copy.BrokerReceipt = ""
	copy.ReconciledAt = 0
	copy.RestoredUntrusted = false
	copy.UpdatedAt = 0
	copy.Reasons = nil
	return Revision(copy)
}

func postureRevision(posture Posture) string {
	copy := posture
	copy.Revision = ""
	copy.ObservedAt, copy.ExpiresAt = 0, 0
	return Revision(copy)
}

func SetPostureRevision(posture *Posture) {
	if posture != nil {
		posture.Revision = postureRevision(*posture)
	}
}

func Revision(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func safeCode(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func boundedCodes(values []string, limit int) bool {
	if len(values) > 32 {
		return false
	}
	for index, value := range values {
		if !safeCode(value, limit) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func boundedPaths(values []string) bool {
	if len(values) > 32 {
		return false
	}
	for index, value := range values {
		if len(value) == 0 || len(value) > 256 || value[0] != '/' || strings.ContainsAny(value, "\r\n\t\x00") || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}
