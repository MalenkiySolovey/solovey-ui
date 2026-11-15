package fronting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	NginxRuntimeIdentitySchemaV2 = "solovey-ui/nginx-runtime-identity/v2"
	MaxConfigureArgumentsV2      = 128
	MaxConfigureArgumentBytesV2  = 256
	MaxLoadedDynamicModulesV2    = 128
	MaxRuntimeReasonCodesV2      = 32
	MaxRuntimeFreshnessV2        = 5 * time.Minute
)

type NginxRuntimeStateV2 string

const (
	NginxNotInstalled          NginxRuntimeStateV2 = "NGINX_NOT_INSTALLED"
	NginxExternalManaged       NginxRuntimeStateV2 = "NGINX_EXTERNAL_MANAGED"
	NginxIdentityUnknown       NginxRuntimeStateV2 = "NGINX_IDENTITY_UNKNOWN"
	NginxStreamUnavailable     NginxRuntimeStateV2 = "STREAM_UNAVAILABLE"
	NginxSSLPrereadUnavailable NginxRuntimeStateV2 = "SSL_PREREAD_UNAVAILABLE"
	NginxProxyProtocolUnproven NginxRuntimeStateV2 = "PROXY_PROTOCOL_UNPROVEN"
	NginxValidationUnavailable NginxRuntimeStateV2 = "VALIDATION_UNAVAILABLE"
	NginxReloadUnavailable     NginxRuntimeStateV2 = "RELOAD_UNAVAILABLE"
	NginxManagedEngineReady    NginxRuntimeStateV2 = "MANAGED_ENGINE_READY"
)

type NginxInstallationClassV2 string

const (
	NginxInstallationManaged     NginxInstallationClassV2 = "SOLOVEY_MANAGED"
	NginxInstallationExternal    NginxInstallationClassV2 = "EXTERNAL_MANAGED"
	NginxInstallationDevelopment NginxInstallationClassV2 = "DEVELOPMENT_READ_ONLY"
	NginxInstallationUnknown     NginxInstallationClassV2 = "UNKNOWN"
)

type CapabilityTruthV2 string

const (
	CapabilitySupportedV2   CapabilityTruthV2 = "SUPPORTED"
	CapabilityUnsupportedV2 CapabilityTruthV2 = "UNSUPPORTED"
	CapabilityUnknownV2     CapabilityTruthV2 = "UNKNOWN"
)

type NginxModuleStateV2 string

const (
	NginxModuleBuiltIn          NginxModuleStateV2 = "BUILT_IN"
	NginxModuleDynamicLoaded    NginxModuleStateV2 = "DYNAMIC_LOADED"
	NginxModuleDynamicNotLoaded NginxModuleStateV2 = "DYNAMIC_NOT_LOADED"
	NginxModuleUnavailable      NginxModuleStateV2 = "UNAVAILABLE"
	NginxModuleUnknown          NginxModuleStateV2 = "UNKNOWN"
)

type NginxModuleCapabilityV2 struct {
	State      NginxModuleStateV2 `json:"state"`
	Effective  CapabilityTruthV2  `json:"effective"`
	Revision   string             `json:"revision"`
	ReasonCode string             `json:"reasonCode,omitempty"`
}

type NginxMethodCapabilityV2 struct {
	Availability CapabilityTruthV2 `json:"availability"`
	Revision     string            `json:"revision"`
}

type NginxRuntimeIdentityV2 struct {
	Schema                           string                   `json:"schema"`
	State                            NginxRuntimeStateV2      `json:"state"`
	States                           []NginxRuntimeStateV2    `json:"states"`
	Version                          string                   `json:"version,omitempty"`
	BuildIdentity                    string                   `json:"buildIdentity,omitempty"`
	InstallationClass                NginxInstallationClassV2 `json:"installationClass"`
	ExecutableDigest                 string                   `json:"executableDigest,omitempty"`
	ExecutableDevice                 uint64                   `json:"executableDevice,omitempty"`
	ExecutableInode                  uint64                   `json:"executableInode,omitempty"`
	ExecutableIdentityRevision       string                   `json:"executableIdentityRevision,omitempty"`
	PlatformIdentityRevision         string                   `json:"platformIdentityRevision,omitempty"`
	ManagedRootIdentityRevision      string                   `json:"managedRootIdentityRevision,omitempty"`
	LoaderConfigOwnershipRevision    string                   `json:"loaderConfigOwnershipRevision,omitempty"`
	ConfigureArgumentsRevision       string                   `json:"configureArgumentsRevision,omitempty"`
	ModuleCapabilityRevision         string                   `json:"moduleCapabilityRevision,omitempty"`
	ValidationMethod                 NginxMethodCapabilityV2  `json:"validationMethod"`
	ReloadMethod                     NginxMethodCapabilityV2  `json:"reloadMethod"`
	ActiveVerification               NginxMethodCapabilityV2  `json:"activeVerification"`
	ProcessVerification              NginxMethodCapabilityV2  `json:"processVerification"`
	ListenerVerification             NginxMethodCapabilityV2  `json:"listenerVerification"`
	Stream                           NginxModuleCapabilityV2  `json:"stream"`
	SSLPreread                       NginxModuleCapabilityV2  `json:"sslPreread"`
	StreamSSL                        NginxModuleCapabilityV2  `json:"streamSsl"`
	StreamRealIP                     NginxModuleCapabilityV2  `json:"streamRealip"`
	ProxyProtocolReceive             NginxMethodCapabilityV2  `json:"proxyProtocolReceive"`
	ProxyProtocolEmit                NginxMethodCapabilityV2  `json:"proxyProtocolEmit"`
	MasterProcessIdentityRevision    string                   `json:"masterProcessIdentityRevision,omitempty"`
	WorkerSetIdentityRevision        string                   `json:"workerSetIdentityRevision,omitempty"`
	ActiveManagedRevision            string                   `json:"activeManagedRevision,omitempty"`
	HelperProtocolVersion            int                      `json:"helperProtocolVersion,omitempty"`
	HelperVersion                    string                   `json:"helperVersion,omitempty"`
	HelperContractVersion            string                   `json:"helperContractVersion,omitempty"`
	HelperContractRevision           string                   `json:"helperContractRevision,omitempty"`
	ManagementExclusionsRevision     string                   `json:"managementExclusionsRevision,omitempty"`
	CanonicalRuntimeIdentityRevision string                   `json:"canonicalRuntimeIdentityRevision"`
	ObservedAt                       int64                    `json:"observedAt"`
	ExpiresAt                        int64                    `json:"expiresAt"`
	ReasonCodes                      []string                 `json:"reasonCodes,omitempty"`
}

type NginxVersionObservationV2 struct {
	Version            string   `json:"-"`
	ConfigureArguments []string `json:"-"`
}

// NginxVersionReaderV2 is deliberately narrower than a command runner. Its
// implementation may perform only the existing bounded version observation.
type NginxVersionReaderV2 interface {
	ReadNginxVersion(context.Context, string) (NginxVersionObservationV2, error)
}

type NginxRuntimeInspectionConfigV2 struct {
	CandidatePaths                []string `json:"-"`
	AllowedExecutableRoots        []string `json:"-"`
	ManagedRootPath               string   `json:"-"`
	ControlledConfigPath          string   `json:"-"`
	InstallationClass             NginxInstallationClassV2
	LoadedDynamicModules          []string
	ValidationMethod              NginxMethodCapabilityV2
	ReloadMethod                  NginxMethodCapabilityV2
	ActiveVerification            NginxMethodCapabilityV2
	ProcessVerification           NginxMethodCapabilityV2
	ListenerVerification          NginxMethodCapabilityV2
	ProxyProtocolReceive          NginxMethodCapabilityV2
	ProxyProtocolEmit             NginxMethodCapabilityV2
	MasterProcessIdentityRevision string
	WorkerSetIdentityRevision     string
	ActiveManagedRevision         string
	HelperProtocolVersion         int
	HelperVersion                 string
	HelperContractVersion         string
	HelperContractRevision        string
	ManagementExclusionsRevision  string
	ObservedAt                    time.Time
	ExpiresAt                     time.Time
}

func copyRuntimeMethods(identity *NginxRuntimeIdentityV2, config NginxRuntimeInspectionConfigV2) {
	identity.ValidationMethod = normalizeMethodV2(config.ValidationMethod)
	identity.ReloadMethod = normalizeMethodV2(config.ReloadMethod)
	identity.ActiveVerification = normalizeMethodV2(config.ActiveVerification)
	identity.ProcessVerification = normalizeMethodV2(config.ProcessVerification)
	identity.ListenerVerification = normalizeMethodV2(config.ListenerVerification)
	identity.ProxyProtocolReceive = normalizeMethodV2(config.ProxyProtocolReceive)
	identity.ProxyProtocolEmit = normalizeMethodV2(config.ProxyProtocolEmit)
	identity.MasterProcessIdentityRevision = safeDigestV2(config.MasterProcessIdentityRevision)
	identity.WorkerSetIdentityRevision = safeDigestV2(config.WorkerSetIdentityRevision)
	identity.ActiveManagedRevision = safeDigestV2(config.ActiveManagedRevision)
	identity.HelperProtocolVersion = config.HelperProtocolVersion
	identity.HelperVersion = safeRuntimeTokenV2(config.HelperVersion, 64)
	identity.HelperContractVersion = safeRuntimeTokenV2(config.HelperContractVersion, 64)
	identity.HelperContractRevision = safeDigestV2(config.HelperContractRevision)
	identity.ManagementExclusionsRevision = safeDigestV2(config.ManagementExclusionsRevision)
}

func normalizeNginxVersionV2(value string) (string, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "nginx/"))
	if value == "" || len(value) > 64 {
		return "", false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || strings.ContainsRune(".+~-", r) {
			continue
		}
		return "", false
	}
	return value, true
}

func normalizeConfigureArgumentsV2(values []string) ([]string, bool) {
	if len(values) > MaxConfigureArgumentsV2 {
		return nil, false
	}
	result := append([]string(nil), values...)
	for index, value := range result {
		if value == "" || value != strings.TrimSpace(value) || len(value) > MaxConfigureArgumentBytesV2 || strings.ContainsAny(value, "\x00\r\n\t") {
			return nil, false
		}
		result[index] = value
	}
	sort.Strings(result)
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, false
		}
	}
	return result, true
}

func moduleCapabilityV2(name string, arguments, loaded []string) NginxModuleCapabilityV2 {
	builtIn, dynamic := false, false
	for _, argument := range arguments {
		switch name {
		case "stream":
			builtIn = builtIn || argument == "--with-stream"
			dynamic = dynamic || argument == "--with-stream=dynamic"
		case "ssl_preread":
			builtIn = builtIn || argument == "--with-stream_ssl_preread_module"
			dynamic = dynamic || argument == "--with-stream_ssl_preread_module=dynamic"
		case "ssl":
			builtIn = builtIn || argument == "--with-stream_ssl_module"
			dynamic = dynamic || argument == "--with-stream_ssl_module=dynamic"
		case "realip":
			builtIn = builtIn || argument == "--with-stream_realip_module"
			dynamic = dynamic || argument == "--with-stream_realip_module=dynamic"
		}
	}
	loadedSet, loadStateKnown := loadedDynamicModuleSetV2(loaded)
	value := NginxModuleCapabilityV2{State: NginxModuleUnavailable, Effective: CapabilityUnsupportedV2, ReasonCode: "module_unavailable"}
	switch {
	case builtIn && dynamic:
		value.State, value.Effective, value.ReasonCode = NginxModuleUnknown, CapabilityUnknownV2, "module_declaration_ambiguous"
	case builtIn:
		value.State, value.Effective, value.ReasonCode = NginxModuleBuiltIn, CapabilitySupportedV2, ""
	case dynamic && !loadStateKnown:
		value.State, value.Effective, value.ReasonCode = NginxModuleUnknown, CapabilityUnknownV2, "dynamic_module_load_state_unknown"
	case dynamic && loadedSet[name]:
		value.State, value.Effective, value.ReasonCode = NginxModuleDynamicLoaded, CapabilitySupportedV2, ""
	case dynamic:
		value.State, value.Effective, value.ReasonCode = NginxModuleDynamicNotLoaded, CapabilityUnsupportedV2, "dynamic_module_not_loaded"
	}
	value.Revision = v2Revision(struct {
		Name      string
		State     NginxModuleStateV2
		Effective CapabilityTruthV2
	}{name, value.State, value.Effective})
	return value
}

func loadedDynamicModuleSetV2(values []string) (map[string]bool, bool) {
	if values == nil || len(values) > MaxLoadedDynamicModulesV2 {
		return nil, false
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if safeRuntimeTokenV2(value, 128) != value || result[value] {
			return nil, false
		}
		result[value] = true
	}
	return result, true
}

func moduleCapabilityValidV2(name string, value NginxModuleCapabilityV2) bool {
	expectedReason := ""
	switch value.State {
	case NginxModuleBuiltIn, NginxModuleDynamicLoaded:
		if value.Effective != CapabilitySupportedV2 {
			return false
		}
	case NginxModuleDynamicNotLoaded:
		if value.Effective != CapabilityUnsupportedV2 {
			return false
		}
		expectedReason = "dynamic_module_not_loaded"
	case NginxModuleUnavailable:
		if value.Effective != CapabilityUnsupportedV2 {
			return false
		}
		expectedReason = "module_unavailable"
	case NginxModuleUnknown:
		if value.Effective != CapabilityUnknownV2 || (value.ReasonCode != "dynamic_module_load_state_unknown" && value.ReasonCode != "module_declaration_ambiguous") {
			return false
		}
		expectedReason = value.ReasonCode
	default:
		return false
	}
	if value.ReasonCode != expectedReason {
		return false
	}
	return value.Revision == v2Revision(struct {
		Name      string
		State     NginxModuleStateV2
		Effective CapabilityTruthV2
	}{name, value.State, value.Effective})
}

func normalizeMethodV2(value NginxMethodCapabilityV2) NginxMethodCapabilityV2 {
	if value.Availability != CapabilitySupportedV2 && value.Availability != CapabilityUnsupportedV2 && value.Availability != CapabilityUnknownV2 {
		value.Availability = CapabilityUnknownV2
	}
	value.Revision = safeDigestV2(value.Revision)
	if value.Revision == "" {
		value.Revision = v2Revision(value.Availability)
	}
	return value
}

func finalizeRuntimeIdentity(value NginxRuntimeIdentityV2) NginxRuntimeIdentityV2 {
	states := []NginxRuntimeStateV2{}
	coreReady := value.InstallationClass == NginxInstallationManaged && value.ExecutableIdentityRevision != "" &&
		value.ManagedRootIdentityRevision != "" && value.LoaderConfigOwnershipRevision != "" && value.Stream.Effective == CapabilitySupportedV2 &&
		value.ValidationMethod.Availability == CapabilitySupportedV2 && value.ReloadMethod.Availability == CapabilitySupportedV2 &&
		value.ActiveVerification.Availability == CapabilitySupportedV2 && value.ProcessVerification.Availability == CapabilitySupportedV2 &&
		value.ListenerVerification.Availability == CapabilitySupportedV2 && value.MasterProcessIdentityRevision != "" && value.WorkerSetIdentityRevision != "" &&
		value.ActiveManagedRevision != "" && value.HelperProtocolVersion > 0 && value.HelperVersion != "" && value.HelperContractVersion != "" &&
		value.HelperContractRevision != "" && value.ManagementExclusionsRevision != "" && len(value.ReasonCodes) == 0
	if value.State == NginxNotInstalled || value.State == NginxExternalManaged {
		states = append(states, value.State)
	} else if !coreReady {
		if value.Stream.Effective == CapabilityUnsupportedV2 {
			states = append(states, NginxStreamUnavailable)
		}
		if value.ValidationMethod.Availability != CapabilitySupportedV2 {
			states = append(states, NginxValidationUnavailable)
		}
		if value.ReloadMethod.Availability != CapabilitySupportedV2 {
			states = append(states, NginxReloadUnavailable)
		}
		states = append(states, NginxIdentityUnknown)
		value.State = NginxIdentityUnknown
	} else {
		value.State = NginxManagedEngineReady
		states = append(states, NginxManagedEngineReady)
	}
	if value.SSLPreread.Effective != CapabilitySupportedV2 {
		states = append(states, NginxSSLPrereadUnavailable)
	}
	if value.ProxyProtocolEmit.Availability != CapabilitySupportedV2 || value.ProxyProtocolReceive.Availability != CapabilitySupportedV2 {
		states = append(states, NginxProxyProtocolUnproven)
	}
	value.States = canonicalRuntimeStatesV2(states)
	value.ReasonCodes = canonicalRuntimeReasonsV2(value.ReasonCodes)
	value.CanonicalRuntimeIdentityRevision = v2Revision(runtimeIdentityRevisionInput(value))
	return value
}

func (value NginxRuntimeIdentityV2) Validate(now time.Time) error {
	if value.Schema != NginxRuntimeIdentitySchemaV2 || !runtimeStateValidV2(value.State) || value.ObservedAt <= 0 ||
		value.ExpiresAt <= value.ObservedAt || value.ExpiresAt-value.ObservedAt > int64(MaxRuntimeFreshnessV2/time.Second) ||
		value.ExpiresAt <= now.UTC().Unix() || !validRuntimeReasonsV2(value.ReasonCodes) ||
		!frontingHexV2(value.CanonicalRuntimeIdentityRevision) || value.CanonicalRuntimeIdentityRevision != v2Revision(runtimeIdentityRevisionInput(value)) {
		return errors.New("nginx_runtime_identity_v2_invalid")
	}
	if !runtimeStatesCanonicalV2(value.States) || !runtimeIdentityStateConsistentV2(value) ||
		value.State == NginxManagedEngineReady && !managedRuntimeIdentityCompleteV2(value) {
		return errors.New("nginx_runtime_identity_v2_incomplete")
	}
	return nil
}

func runtimeIdentityStateConsistentV2(value NginxRuntimeIdentityV2) bool {
	if value.State == NginxExternalManaged && value.InstallationClass != NginxInstallationExternal {
		return false
	}
	if value.State == NginxNotInstalled && (value.Version != "" || value.BuildIdentity != "" || value.ExecutableIdentityRevision != "") {
		return false
	}
	if value.Version != "" && (!frontingHexV2(value.ConfigureArgumentsRevision) || value.BuildIdentity != "nginx/"+value.Version+"/"+value.ConfigureArgumentsRevision[:16]) {
		return false
	}
	modules := []struct {
		name  string
		value NginxModuleCapabilityV2
	}{
		{"stream", value.Stream},
		{"ssl_preread", value.SSLPreread},
		{"ssl", value.StreamSSL},
		{"realip", value.StreamRealIP},
	}
	moduleFactsPresent := value.ModuleCapabilityRevision != ""
	for _, module := range modules {
		if module.value.State != "" || module.value.Effective != "" || module.value.Revision != "" || module.value.ReasonCode != "" {
			moduleFactsPresent = true
		}
	}
	if moduleFactsPresent {
		for _, module := range modules {
			if !moduleCapabilityValidV2(module.name, module.value) {
				return false
			}
		}
		if value.ModuleCapabilityRevision != v2Revision([]NginxModuleCapabilityV2{value.Stream, value.SSLPreread, value.StreamSSL, value.StreamRealIP}) {
			return false
		}
	}
	expected := finalizeRuntimeIdentity(value)
	return expected.State == value.State && equalRuntimeStatesV2(expected.States, value.States) &&
		equalRuntimeReasonsV2(expected.ReasonCodes, value.ReasonCodes)
}

func equalRuntimeStatesV2(left, right []NginxRuntimeStateV2) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalRuntimeReasonsV2(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func managedRuntimeIdentityCompleteV2(value NginxRuntimeIdentityV2) bool {
	methods := []NginxMethodCapabilityV2{value.ValidationMethod, value.ReloadMethod, value.ActiveVerification,
		value.ProcessVerification, value.ListenerVerification}
	for _, method := range methods {
		if method.Availability != CapabilitySupportedV2 || !frontingHexV2(method.Revision) {
			return false
		}
	}
	modules := []struct {
		name  string
		value NginxModuleCapabilityV2
	}{{"stream", value.Stream}, {"ssl_preread", value.SSLPreread}, {"ssl", value.StreamSSL}, {"realip", value.StreamRealIP}}
	for _, module := range modules {
		if !moduleCapabilityValidV2(module.name, module.value) {
			return false
		}
	}
	return value.InstallationClass == NginxInstallationManaged && value.Version != "" && value.BuildIdentity != "" &&
		frontingHexV2(value.ExecutableDigest) && frontingHexV2(value.ExecutableIdentityRevision) && frontingHexV2(value.PlatformIdentityRevision) &&
		frontingHexV2(value.ManagedRootIdentityRevision) && frontingHexV2(value.LoaderConfigOwnershipRevision) &&
		frontingHexV2(value.ConfigureArgumentsRevision) && value.ModuleCapabilityRevision == v2Revision([]NginxModuleCapabilityV2{value.Stream, value.SSLPreread, value.StreamSSL, value.StreamRealIP}) &&
		value.Stream.Effective == CapabilitySupportedV2 && frontingHexV2(value.MasterProcessIdentityRevision) &&
		frontingHexV2(value.WorkerSetIdentityRevision) && frontingHexV2(value.ActiveManagedRevision) &&
		value.HelperProtocolVersion > 0 && value.HelperVersion != "" && value.HelperContractVersion != "" &&
		frontingHexV2(value.HelperContractRevision) && frontingHexV2(value.ManagementExclusionsRevision) && len(value.ReasonCodes) == 0
}

func runtimeStatesCanonicalV2(values []NginxRuntimeStateV2) bool {
	if len(values) == 0 {
		return false
	}
	for index, value := range values {
		if !runtimeStateValidV2(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func runtimeIdentityRevisionInput(value NginxRuntimeIdentityV2) NginxRuntimeIdentityV2 {
	value.CanonicalRuntimeIdentityRevision = ""
	value.ObservedAt, value.ExpiresAt = 0, 0
	return value
}

func runtimeStateValidV2(value NginxRuntimeStateV2) bool {
	switch value {
	case NginxNotInstalled, NginxExternalManaged, NginxIdentityUnknown, NginxStreamUnavailable,
		NginxSSLPrereadUnavailable, NginxProxyProtocolUnproven, NginxValidationUnavailable,
		NginxReloadUnavailable, NginxManagedEngineReady:
		return true
	default:
		return false
	}
}

func canonicalRuntimeStatesV2(values []NginxRuntimeStateV2) []NginxRuntimeStateV2 {
	seen := map[NginxRuntimeStateV2]bool{}
	result := make([]NginxRuntimeStateV2, 0, len(values))
	for _, value := range values {
		if runtimeStateValidV2(value) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func canonicalRuntimeReasonsV2(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, min(len(values), MaxRuntimeReasonCodesV2))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !safeRuntimeReasonV2(value) {
			value = "runtime_reason_invalid"
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == MaxRuntimeReasonCodesV2 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func validRuntimeReasonsV2(values []string) bool {
	if len(values) > MaxRuntimeReasonCodesV2 {
		return false
	}
	for index, value := range values {
		if !safeRuntimeReasonV2(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func safeRuntimeReasonV2(value string) bool { return safeRuntimeTokenV2(value, 64) != "" }

func safeRuntimeTokenV2(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return ""
	}
	return value
}

func safeDigestV2(value string) string {
	if frontingHexV2(value) {
		return value
	}
	return ""
}

func frontingHexV2(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func v2Revision(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// NewNginxRuntimeIdentityV2 seeds a bounded identity from typed capability metadata.
func NewNginxRuntimeIdentityV2(config NginxRuntimeInspectionConfigV2) NginxRuntimeIdentityV2 {
	now := config.ObservedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expires := config.ExpiresAt.UTC()
	if expires.IsZero() {
		expires = now.Add(MaxRuntimeFreshnessV2)
	}
	identity := NginxRuntimeIdentityV2{Schema: NginxRuntimeIdentitySchemaV2, State: NginxIdentityUnknown,
		InstallationClass: config.InstallationClass, ObservedAt: now.Unix(), ExpiresAt: expires.Unix()}
	copyRuntimeMethods(&identity, config)
	return identity
}

// ApplyNginxBuildObservationV2 applies the canonical version and module rules to owner observations.
func ApplyNginxBuildObservationV2(identity NginxRuntimeIdentityV2, observation NginxVersionObservationV2, loaded []string) (NginxRuntimeIdentityV2, bool) {
	version, ok := normalizeNginxVersionV2(observation.Version)
	arguments, argumentsOK := normalizeConfigureArgumentsV2(observation.ConfigureArguments)
	if !ok {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_version_unparseable")
		return identity, false
	}
	if !argumentsOK {
		identity.ReasonCodes = append(identity.ReasonCodes, "nginx_configure_arguments_invalid")
		return identity, false
	}
	identity.Version = version
	identity.ConfigureArgumentsRevision = v2Revision(arguments)
	identity.BuildIdentity = "nginx/" + version + "/" + identity.ConfigureArgumentsRevision[:16]
	identity.Stream = moduleCapabilityV2("stream", arguments, loaded)
	identity.SSLPreread = moduleCapabilityV2("ssl_preread", arguments, loaded)
	identity.StreamSSL = moduleCapabilityV2("ssl", arguments, loaded)
	identity.StreamRealIP = moduleCapabilityV2("realip", arguments, loaded)
	identity.ModuleCapabilityRevision = v2Revision([]NginxModuleCapabilityV2{identity.Stream, identity.SSLPreread, identity.StreamSSL, identity.StreamRealIP})
	return identity, true
}

// FinalizeNginxRuntimeIdentityV2 canonicalizes the typed owner observation without performing I/O.
func FinalizeNginxRuntimeIdentityV2(value NginxRuntimeIdentityV2) NginxRuntimeIdentityV2 {
	return finalizeRuntimeIdentity(value)
}

// NginxRuntimeFactRevisionV2 uses the same canonical revision format for OS identity facts.
func NginxRuntimeFactRevisionV2(value any) string { return v2Revision(value) }

// CanonicalNginxRuntimeReasonsV2 bounds and orders inspection reasons before publication.
func CanonicalNginxRuntimeReasonsV2(reasons []string) []string {
	return canonicalRuntimeReasonsV2(reasons)
}

// NginxRuntimeInspectionSourceV2 observes only the selected nginx identity and managed contour.
// Filesystem and platform attestation remain with its Backend implementation.
type NginxRuntimeInspectionSourceV2 interface {
	Inspect(context.Context, NginxRuntimeInspectionConfigV2, NginxVersionObservationV2) (NginxRuntimeIdentityV2, error)
}
