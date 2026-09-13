package hostsurface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const SchemaV1 = "solovey-ui/host-surface-fact/v1"

type Network string
type Family string
type Exposure string
type OwnershipMode string
type Classification string
type ManagementService string

const (
	NetworkTCP     Network = "tcp"
	NetworkUDP     Network = "udp"
	NetworkUnknown Network = "unknown"

	FamilyIPv4    Family = "ipv4"
	FamilyIPv6    Family = "ipv6"
	FamilyUnknown Family = "unknown"

	ExposurePublic  Exposure = "public"
	ExposurePrivate Exposure = "private"
	ExposureLocal   Exposure = "local"
	ExposureUnknown Exposure = "unknown"

	OwnershipManaged         OwnershipMode = "managed"
	OwnershipExternalManaged OwnershipMode = "external_managed"
	OwnershipUnmanaged       OwnershipMode = "unmanaged"

	ClassificationExpectedManaged  Classification = "EXPECTED_MANAGED"
	ClassificationExpectedExternal Classification = "EXPECTED_EXTERNAL"
	ClassificationLocalOnly        Classification = "LOCAL_ONLY"
	ClassificationUnexpectedPublic Classification = "UNEXPECTED_PUBLIC"
	ClassificationUnknownOwner     Classification = "UNKNOWN_OWNER"
	ClassificationManagedExact     Classification = "MANAGED_EXACT"
	ClassificationForeign          Classification = "FOREIGN"
	ClassificationUnobserved       Classification = "UNOBSERVED"
	ClassificationStale            Classification = "STALE"

	ManagementServiceSSH ManagementService = "ssh"
)

type SemanticOwnerFactV1 struct {
	ManagementService ManagementService        `json:"managementService"`
	Source            string                   `json:"source"`
	Revision          string                   `json:"revision"`
	AuthorityRevision string                   `json:"authorityRevision"`
	Socket            ListenerSocketIdentityV1 `json:"socket"`
}

type ProcessFact struct {
	ProviderRevision string `json:"providerRevision,omitempty"`
	EvidenceRevision string `json:"evidenceRevision,omitempty"`
	PID              *int   `json:"pid,omitempty"`
	ParentPID        *int   `json:"parentPid,omitempty"`
	SessionID        *int   `json:"sessionId,omitempty"`
	StartTime        string `json:"startTime,omitempty"`
	// ExeDigest is the raw SHA-256 of the executable bytes reached through
	// /proc/<pid>/exe, not the structured Revision of this process fact.
	ExeDigest    string `json:"exeDigest,omitempty"`
	Executable   string `json:"executable,omitempty"`
	ExeDevice    uint64 `json:"exeDevice,omitempty"`
	ExeInode     uint64 `json:"exeInode,omitempty"`
	UID          *int   `json:"uid,omitempty"`
	GID          *int   `json:"gid,omitempty"`
	ControlGroup string `json:"controlGroup,omitempty"`
}

type ServiceFact struct {
	SupervisorRevision string   `json:"supervisorRevision,omitempty"`
	CgroupAvailability string   `json:"cgroupAvailability,omitempty"`
	CgroupPolicy       string   `json:"cgroupPolicy,omitempty"`
	CgroupRevision     string   `json:"cgroupRevision,omitempty"`
	SystemdUnit        string   `json:"systemdUnit,omitempty"`
	MainPID            *int     `json:"mainPid,omitempty"`
	FragmentPath       string   `json:"fragmentPath,omitempty"`
	FragmentSHA256     string   `json:"fragmentSha256,omitempty"`
	ActiveState        string   `json:"activeState,omitempty"`
	SubState           string   `json:"subState,omitempty"`
	ControlGroup       string   `json:"controlGroup,omitempty"`
	StartMonotonicUsec uint64   `json:"startMonotonicUsec,omitempty"`
	ContainerCgroup    string   `json:"containerCgroup,omitempty"`
	ProcdService       string   `json:"procdService,omitempty"`
	ProcdInstance      string   `json:"procdInstance,omitempty"`
	ProcdCommand       []string `json:"procdCommand,omitempty"`
	ProcdUser          string   `json:"procdUser,omitempty"`
	ProcdGroup         string   `json:"procdGroup,omitempty"`
}

// IdentifiesActiveProcess reports whether the supervisor-neutral service
// projection identifies the supplied active main process. Consumers of the
// generic host-surface boundary must not select an installed supervisor
// schema themselves.
func (f ServiceFact) IdentifiesActiveProcess(pid int) bool {
	if pid <= 1 || f.MainPID == nil || *f.MainPID != pid || f.ActiveState != "active" || f.SubState != "running" {
		return false
	}
	if procdOwnerService(f) {
		return f.ProcdService != "" && f.ProcdInstance != "" && f.SystemdUnit == ""
	}
	return f.SystemdUnit != ""
}

type HostSurfaceFactV1 struct {
	Schema                string               `json:"schema"`
	ID                    string               `json:"id"`
	Network               Network              `json:"network"`
	Family                Family               `json:"family"`
	Bind                  string               `json:"bind"`
	Port                  uint16               `json:"port"`
	Protocol              string               `json:"protocol"`
	Exposure              Exposure             `json:"exposure"`
	SocketInode           string               `json:"socketInode,omitempty"`
	SocketCookie          uint64               `json:"socketCookie,omitempty"`
	Process               ProcessFact          `json:"process"`
	Service               ServiceFact          `json:"service"`
	ListenerOwner         *ListenerOwnerFactV1 `json:"listenerOwner,omitempty"`
	SemanticOwner         *SemanticOwnerFactV1 `json:"semanticOwner,omitempty"`
	RegisteredResourceID  string               `json:"registeredResourceId,omitempty"`
	DesiredOwner          string               `json:"desiredOwner,omitempty"`
	OwnershipMode         OwnershipMode        `json:"ownershipMode"`
	FirstSeen             int64                `json:"firstSeen"`
	LastSeen              int64                `json:"lastSeen"`
	ExpiresAt             int64                `json:"expiresAt"`
	Source                string               `json:"source"`
	ConfidenceBP          int                  `json:"confidenceBp"`
	ConfigurationRevision string               `json:"configurationRevision"`
	Classification        Classification       `json:"classification"`
	ReasonCodes           []string             `json:"reasonCodes,omitempty"`
	Stale                 bool                 `json:"stale"`
	Truncated             bool                 `json:"truncated"`
}

func StableID(value HostSurfaceFactV1) string {
	payload, _ := json.Marshal(struct {
		Network  Network `json:"network"`
		Family   Family  `json:"family"`
		Bind     string  `json:"bind"`
		Port     uint16  `json:"port"`
		Source   string  `json:"source"`
		Resource string  `json:"resource,omitempty"`
		Cookie   uint64  `json:"cookie,omitempty"`
	}{value.Network, value.Family, strings.TrimSpace(value.Bind), value.Port, strings.TrimSpace(value.Source), strings.TrimSpace(value.RegisteredResourceID), value.SocketCookie})
	sum := sha256.Sum256(payload)
	return "hostsurface:" + hex.EncodeToString(sum[:16])
}

func (f HostSurfaceFactV1) IsStale(now time.Time) bool {
	return f.Stale || (f.ExpiresAt > 0 && f.ExpiresAt <= now.UTC().Unix())
}

type Limits struct {
	MaxSockets       int
	MaxCandidatePIDs int
	MaxDecodedBytes  int64
	Timeout          time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxSockets: 4096, MaxCandidatePIDs: 8192, MaxDecodedBytes: 4 << 20, Timeout: 5 * time.Second}
}

type Observation struct {
	Facts                    []HostSurfaceFactV1
	Truncated                bool
	ReasonCodes              []string
	OwnerObservationRevision string
}

type Snapshot struct {
	GeneratedAt              int64               `json:"generatedAt"`
	Facts                    []HostSurfaceFactV1 `json:"facts"`
	Truncated                bool                `json:"truncated"`
	ReasonCodes              []string            `json:"reasonCodes,omitempty"`
	OwnerObservationRevision string              `json:"ownerObservationRevision,omitempty"`
}
