package hostsurface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"path"
	"sort"
	"strings"
	"time"
)

const ListenerOwnerFactSchemaV1 = "solovey-ui/listener-owner-fact/v1"

type ListenerSocketIdentityV1 struct {
	Network          Network  `json:"network"`
	Family           Family   `json:"family"`
	Bind             string   `json:"bind"`
	Port             uint16   `json:"port"`
	Inode            string   `json:"inode"`
	Cookie           uint64   `json:"cookie"`
	Wildcard         bool     `json:"wildcard"`
	IPv6Only         *bool    `json:"ipv6Only,omitempty"`
	CoverageFamilies []Family `json:"coverageFamilies"`
}

type ListenerApplicationIdentityV1 struct {
	InstanceID                 string `json:"instanceId"`
	SourceRevision             string `json:"sourceRevision"`
	ArtifactRevision           string `json:"artifactRevision"`
	DeploymentID               string `json:"deploymentId"`
	OwnerContractRevision      string `json:"ownerContractRevision"`
	RuntimeRootBindingRevision string `json:"runtimeRootBindingRevision"`
	ExpectedExecutableSHA256   string `json:"expectedExecutableSha256"`
	ServiceIdentity            string `json:"serviceIdentity"`
	ResourceID                 string `json:"resourceId"`
	ResourceOwnerRevision      string `json:"resourceOwnerRevision"`
	ConfigurationRevision      string `json:"configurationRevision"`
}

// ListenerOwnerFactV1 is a complete exact proof. Partial observations are
// represented by typed reason codes on the surrounding observation, never by
// a weakened fact that could be mistaken for MANAGED_EXACT.
type ListenerOwnerFactV1 struct {
	Schema              string                        `json:"schema"`
	ObservationRevision string                        `json:"observationRevision"`
	Socket              ListenerSocketIdentityV1      `json:"socket"`
	Process             ProcessFact                   `json:"process"`
	Service             ServiceFact                   `json:"service"`
	Application         ListenerApplicationIdentityV1 `json:"application"`
	ObservedAt          int64                         `json:"observedAt"`
	ExpiresAt           int64                         `json:"expiresAt"`
}

func (f *ListenerOwnerFactV1) Seal() {
	if f == nil {
		return
	}
	f.Socket.CoverageFamilies = normalizedOwnerFamilies(f.Socket.CoverageFamilies)
	copy := *f
	copy.ObservationRevision = ""
	copy.ObservedAt, copy.ExpiresAt = 0, 0
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	f.ObservationRevision = hex.EncodeToString(sum[:])
}

func (f ListenerOwnerFactV1) Valid(now time.Time) bool {
	if f.Schema != ListenerOwnerFactSchemaV1 || !hex64(f.ObservationRevision) || f.ObservedAt <= 0 ||
		f.ExpiresAt <= f.ObservedAt || f.ExpiresAt > f.ObservedAt+60 || f.ExpiresAt <= now.UTC().Unix() {
		return false
	}
	copy := f
	copy.Seal()
	if copy.ObservationRevision != f.ObservationRevision || !validOwnerSocket(f.Socket) || !validOwnerProcess(f.Process) || !validOwnerService(f.Service) || !validOwnerApplication(f.Application) {
		return false
	}
	return f.Process.PID != nil && f.Service.MainPID != nil && *f.Process.PID == *f.Service.MainPID &&
		f.Process.ControlGroup == f.Service.ControlGroup && f.Process.ExeDigest == f.Application.ExpectedExecutableSHA256
}

func validOwnerSocket(value ListenerSocketIdentityV1) bool {
	if value.Network != NetworkTCP && value.Network != NetworkUDP || value.Family != FamilyIPv4 && value.Family != FamilyIPv6 || value.Port == 0 || value.Cookie == 0 || !numericToken(value.Inode) {
		return false
	}
	address, err := netip.ParseAddr(value.Bind)
	if err != nil || (address.Unmap().Is4()) != (value.Family == FamilyIPv4) || address.IsUnspecified() != value.Wildcard {
		return false
	}
	families := normalizedOwnerFamilies(value.CoverageFamilies)
	if len(families) != len(value.CoverageFamilies) || len(families) == 0 {
		return false
	}
	if value.Family == FamilyIPv4 {
		return len(families) == 1 && families[0] == FamilyIPv4 && value.IPv6Only == nil
	}
	if value.IPv6Only == nil {
		return false
	}
	if *value.IPv6Only {
		return len(families) == 1 && families[0] == FamilyIPv6
	}
	return value.Wildcard && len(families) == 2 && families[0] == FamilyIPv4 && families[1] == FamilyIPv6
}

func validOwnerProcess(value ProcessFact) bool {
	if value.PID == nil || value.ParentPID == nil || value.SessionID == nil || value.UID == nil || value.GID == nil ||
		*value.PID <= 1 || *value.ParentPID < 0 || *value.SessionID < 0 || *value.UID < 0 || *value.GID < 0 ||
		!numericToken(value.StartTime) || !hex64(value.ExeDigest) || value.ExeDevice == 0 || value.ExeInode == 0 ||
		!canonicalOwnerPath(value.Executable) || !canonicalOwnerPath(value.ControlGroup) {
		return false
	}
	return true
}

func validOwnerService(value ServiceFact) bool {
	return safeFactToken(value.SystemdUnit, 128) && value.MainPID != nil && *value.MainPID > 1 &&
		canonicalOwnerPath(value.FragmentPath) && hex64(value.FragmentSHA256) && value.ActiveState == "active" && value.SubState == "running" &&
		canonicalOwnerPath(value.ControlGroup) && value.StartMonotonicUsec > 0
}

func validOwnerApplication(value ListenerApplicationIdentityV1) bool {
	return safeFactToken(value.InstanceID, 128) && safeFactToken(value.ServiceIdentity, 128) && safeFactToken(value.ResourceID, 256) &&
		prefixedHex64(value.SourceRevision, "src-") && prefixedHex64(value.ArtifactRevision, "art-") && prefixedHex64(value.DeploymentID, "dep-") &&
		hex64(value.OwnerContractRevision) && hex64(value.RuntimeRootBindingRevision) && hex64(value.ExpectedExecutableSHA256) &&
		hex64(value.ResourceOwnerRevision) && hex64(value.ConfigurationRevision)
}

func normalizedOwnerFamilies(values []Family) []Family {
	seen := map[Family]bool{}
	result := make([]Family, 0, 2)
	for _, value := range values {
		if (value == FamilyIPv4 || value == FamilyIPv6) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func canonicalOwnerPath(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value && value != "/" && len(value) <= 512 && !strings.ContainsAny(value, "\x00\r\n\t")
}

func prefixedHex64(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && hex64(strings.TrimPrefix(value, prefix))
}

func hex64(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func OwnerObservationSetRevision(facts []HostSurfaceFactV1, reasons []string) string {
	type item struct {
		Resource       string
		Classification Classification
		OwnerRevision  string
		Reasons        []string
	}
	values := make([]item, 0, len(facts))
	for _, fact := range facts {
		revision := ""
		if fact.ListenerOwner != nil {
			revision = fact.ListenerOwner.ObservationRevision
		}
		copyReasons := append([]string(nil), fact.ReasonCodes...)
		sort.Strings(copyReasons)
		values = append(values, item{fact.RegisteredResourceID, fact.Classification, revision, copyReasons})
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].Resource+"\x00"+values[i].OwnerRevision+"\x00"+string(values[i].Classification) < values[j].Resource+"\x00"+values[j].OwnerRevision+"\x00"+string(values[j].Classification)
	})
	copyReasons := append([]string(nil), reasons...)
	sort.Strings(copyReasons)
	data, _ := json.Marshal(struct {
		Facts   []item
		Reasons []string
	}{values, copyReasons})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
