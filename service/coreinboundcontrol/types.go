package coreinboundcontrol

const (
	RuntimeIdentitySchemaV1  = "solovey-ui/core-runtime-identity/v1"
	InboundSnapshotSchemaV1  = "solovey-ui/inbound-fallback-snapshot/v1"
	QUICBuildFeatureSchemaV1 = "solovey-ui/core-quic-build-feature/v1"

	PinnedSingBoxModule         = "github.com/sagernet/sing-box"
	PinnedSingBoxVersion        = "v1.13.14"
	PinnedSingBoxModuleSum      = "h1:p9/eqwilCgzyR/DpKM8hq7ppvzPIq1QMLgZWT3Cbg10="
	PinnedSingBoxSourceRevision = "25a600db24f7680ad9806ce5427bd0ab8afe1114"
	PinnedUTLSModule            = "github.com/metacubex/utls"
	PinnedUTLSVersion           = "v1.8.4"
	PinnedUTLSModuleSum         = "h1:HmL9nUApDdWSkgUyodfwF6hSjtiwCGGdyhaSpEejKpg="
	PinnedUTLSSourceRevision    = "cf49b0864331e156689feec6363ef9bf518a5ac7"

	CapabilityResolverRevisionV1            = "389ac746da5c21bab9f74c46310256ea020f2860734d731c9539f299741a7673"
	BuildProfileWithUTLSRevision            = "2573ebf675608c4e589ebc062ecf46078f77244489cb5a1578ecb2b649ae0608"
	BuildProfileWithoutUTLSRevision         = "a55ee5ad8f16d0ab527a612e4a89e2170b2751af200430a90022df99cc33192d"
	PinnedRuntimeIdentityWithUTLSRevisionV1 = "b770558377c5d4512275dbe0958b7174af751e6d2666206bfc4aa5de2a189af1"
)

type ReasonCode string

const (
	ReasonBuildInfoMissing               ReasonCode = "build_info_missing"
	ReasonSingBoxModuleMissing           ReasonCode = "sing_box_module_missing"
	ReasonSingBoxModuleReplaced          ReasonCode = "sing_box_module_replaced"
	ReasonSingBoxVersionMismatch         ReasonCode = "sing_box_version_mismatch"
	ReasonSingBoxSumMismatch             ReasonCode = "sing_box_sum_mismatch"
	ReasonUTLSModuleMissing              ReasonCode = "utls_module_missing"
	ReasonUTLSModuleReplaced             ReasonCode = "utls_module_replaced"
	ReasonUTLSVersionMismatch            ReasonCode = "utls_version_mismatch"
	ReasonUTLSSumMismatch                ReasonCode = "utls_sum_mismatch"
	ReasonBuildProfileUnknown            ReasonCode = "build_profile_unknown"
	ReasonWithUTLSInconsistent           ReasonCode = "with_utls_inconsistent"
	ReasonResolverRevisionMismatch       ReasonCode = "capability_resolver_revision_mismatch"
	ReasonRuntimeIdentityUnverified      ReasonCode = "runtime_identity_unverified"
	ReasonInboundOptionsMalformed        ReasonCode = "inbound_options_malformed"
	ReasonTLSReferenceMissing            ReasonCode = "tls_reference_missing"
	ReasonTLSReferenceMismatch           ReasonCode = "tls_reference_mismatch"
	ReasonTLSOptionsMalformed            ReasonCode = "tls_options_malformed"
	ReasonInboundShapeUnknown            ReasonCode = "inbound_shape_unknown"
	ReasonListenerNetworkUnknown         ReasonCode = "listener_network_unknown"
	ReasonListenerPortInvalid            ReasonCode = "listener_port_invalid"
	ReasonTagUnsafe                      ReasonCode = "inbound_tag_unsafe"
	ReasonVLESSFallbackUnavailable       ReasonCode = "vless_generic_fallback_unavailable"
	ReasonVLESSOrdinaryTLSUnsupported    ReasonCode = "vless_ordinary_tls_unsupported"
	ReasonRealityRequired                ReasonCode = "vless_reality_required"
	ReasonRealityTargetNotTCP            ReasonCode = "reality_target_not_tcp"
	ReasonFallbackTargetNotTCP           ReasonCode = "fallback_target_not_tcp"
	ReasonWithUTLSRequired               ReasonCode = "with_utls_required"
	ReasonV2RayTransportUnsupported      ReasonCode = "v2ray_transport_unsupported"
	ReasonTrojanTLSRequired              ReasonCode = "trojan_tls_required"
	ReasonTrojanFallbackNotConfigured    ReasonCode = "trojan_fallback_not_configured"
	ReasonTrojanALPNSetUnknown           ReasonCode = "trojan_alpn_set_unknown"
	ReasonTrojanALPNNotExhaustive        ReasonCode = "trojan_alpn_not_exhaustive"
	ReasonTrojanMultiplexUnproven        ReasonCode = "trojan_multiplex_unproven"
	ReasonProtocolOutOfScope             ReasonCode = "protocol_out_of_scope"
	ReasonInboundTypeUnsupported         ReasonCode = "inbound_type_unsupported"
	ReasonEffectiveRuntimeUnavailable    ReasonCode = "effective_runtime_unavailable"
	ReasonEffectiveInboundMissing        ReasonCode = "effective_inbound_missing"
	ReasonEffectiveInboundTypeMismatch   ReasonCode = "effective_inbound_type_mismatch"
	ReasonEffectiveConfigurationUnproven ReasonCode = "effective_configuration_unproven"
)

type RuntimeIdentityState string

const (
	RuntimeIdentityVerified RuntimeIdentityState = "verified"
	RuntimeIdentityUnknown  RuntimeIdentityState = "unknown"
)

type BuildFeatureState string

const (
	BuildFeatureSupported   BuildFeatureState = "SUPPORTED"
	BuildFeatureUnavailable BuildFeatureState = "UNAVAILABLE"
	BuildFeatureUnknown     BuildFeatureState = "UNKNOWN"
)

type QUICBuildFeatureV1 struct {
	Schema               string            `json:"schema"`
	Feature              string            `json:"feature"`
	State                BuildFeatureState `json:"state"`
	RuntimeIdentity      string            `json:"runtimeIdentity"`
	SourceRevision       string            `json:"sourceRevision"`
	ModuleRevision       string            `json:"moduleRevision"`
	BuildProfileRevision string            `json:"buildProfileRevision"`
	ObservationMethod    string            `json:"observationMethod"`
	Revision             string            `json:"revision"`
	ReasonCodes          []ReasonCode      `json:"reasonCodes,omitempty"`
}

type CoreRuntimeIdentityV1 struct {
	Schema                     string               `json:"schema"`
	State                      RuntimeIdentityState `json:"state"`
	SingBoxModule              string               `json:"singBoxModule"`
	SingBoxVersion             string               `json:"singBoxVersion"`
	SingBoxModuleSum           string               `json:"singBoxModuleSum"`
	SingBoxSourceRevision      string               `json:"singBoxSourceRevision"`
	UTLSModule                 string               `json:"utlsModule"`
	UTLSVersion                string               `json:"utlsVersion"`
	UTLSModuleSum              string               `json:"utlsModuleSum"`
	UTLSSourceRevision         string               `json:"utlsSourceRevision"`
	WithUTLS                   bool                 `json:"withUtls"`
	BuildProfileRevision       string               `json:"buildProfileRevision"`
	CapabilityResolverRevision string               `json:"capabilityResolverRevision"`
	IdentityRevision           string               `json:"identityRevision"`
	ReasonCodes                []ReasonCode         `json:"reasonCodes,omitempty"`
}

type RuntimeModuleV1 struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Sum      string `json:"sum"`
	Replaced bool   `json:"replaced"`
}

type RuntimeBuildInputV1 struct {
	Available                  bool              `json:"available"`
	Modules                    []RuntimeModuleV1 `json:"modules"`
	WithUTLS                   bool              `json:"withUtls"`
	BuildProfileRevision       string            `json:"buildProfileRevision"`
	CapabilityResolverRevision string            `json:"capabilityResolverRevision"`
}

type ListenerShapeV1 struct {
	Network       string `json:"network"`
	AddressFamily string `json:"addressFamily"`
	Bind          string `json:"bind"`
	Port          uint16 `json:"port"`
	ProxyProtocol bool   `json:"proxyProtocol"`
}

type AuthenticationShapeV1 struct {
	Known    bool   `json:"known"`
	Expected bool   `json:"expected"`
	Count    int    `json:"count"`
	Revision string `json:"revision,omitempty"`
}

type LocalProxyShapeV1 struct {
	Candidate                    bool                  `json:"candidate"`
	Protocols                    []string              `json:"protocols,omitempty"`
	ProtocolRevision             string                `json:"protocolRevision,omitempty"`
	Authentication               AuthenticationShapeV1 `json:"authentication"`
	SOCKS4Authenticated          bool                  `json:"socks4Authenticated"`
	TLS                          TLSShapeV1            `json:"tls"`
	TLSRevision                  string                `json:"tlsRevision,omitempty"`
	SystemProxyKnown             bool                  `json:"systemProxyKnown"`
	SystemProxyEnabled           bool                  `json:"systemProxyEnabled"`
	SystemProxyRevision          string                `json:"systemProxyRevision,omitempty"`
	DependentUDPAssociation      bool                  `json:"dependentUdpAssociation"`
	StaticUDPListener            bool                  `json:"staticUdpListener"`
	ManagementTargetConfigurable bool                  `json:"managementTargetConfigurable"`
	ArbitraryTargetConfigurable  bool                  `json:"arbitraryTargetConfigurable"`
}

// InterceptionShapeV1 is the secret-free, pinned-core truth needed by a
// neutral interception consumer. It describes only an existing core-owned
// Redirect/TProxy inbound; it never grants authority to mutate that inbound or
// supplies firewall/routing values.
type InterceptionShapeV1 struct {
	Candidate                    bool     `json:"candidate"`
	Kind                         string   `json:"kind,omitempty"`
	EffectiveNetworks            []string `json:"effectiveNetworks,omitempty"`
	EffectiveNetworksRevision    string   `json:"effectiveNetworksRevision,omitempty"`
	LinuxOnly                    bool     `json:"linuxOnly"`
	TransparentSocketRequired    bool     `json:"transparentSocketRequired"`
	OriginalDestinationMechanism string   `json:"originalDestinationMechanism,omitempty"`
	OriginalDestinationPreserved bool     `json:"originalDestinationPreserved"`
	SourcePreserved              bool     `json:"sourcePreserved"`
	PolicyRoutingRequired        bool     `json:"policyRoutingRequired"`
	BoundedUDPFlowState          bool     `json:"boundedUdpFlowState"`
	LocalOutputCapture           bool     `json:"localOutputCapture"`
	TUNOwned                     bool     `json:"tunOwned"`
	SemanticRevision             string   `json:"semanticRevision,omitempty"`
}

type UDPTransportShapeV2 struct {
	EffectiveNetworks        []string     `json:"effectiveNetworks"`
	EffectiveNetworkRevision string       `json:"effectiveNetworkRevision"`
	Class                    string       `json:"class"`
	TransportRevision        string       `json:"transportRevision"`
	SocketIntentRevision     string       `json:"socketIntentRevision"`
	ProtocolOwnedZeroRTT     bool         `json:"protocolOwnedZeroRtt"`
	ProtocolOwnedMigration   bool         `json:"protocolOwnedMigration"`
	ProtocolOwnedCID         bool         `json:"protocolOwnedCid"`
	UDPTimeoutRevision       string       `json:"udpTimeoutRevision"`
	DependentAssociation     bool         `json:"dependentAssociation"`
	DirectSocketActionable   bool         `json:"directSocketActionable"`
	ReasonCodes              []ReasonCode `json:"reasonCodes,omitempty"`
}

type TransportShapeV1 struct {
	Present bool   `json:"present"`
	Type    string `json:"type,omitempty"`
}

type MultiplexShapeV1 struct {
	Present bool `json:"present"`
	Enabled bool `json:"enabled"`
}

type TargetShapeV1 struct {
	Present  bool   `json:"present"`
	Kind     string `json:"kind,omitempty"`
	Revision string `json:"revision,omitempty"`
}

type RealityShapeV1 struct {
	Present   bool          `json:"present"`
	Enabled   bool          `json:"enabled"`
	Handshake TargetShapeV1 `json:"handshake"`
}

type TLSShapeV1 struct {
	Referenced       bool           `json:"referenced"`
	Enabled          bool           `json:"enabled"`
	ServerNameDigest string         `json:"serverNameDigest,omitempty"`
	ALPN             []string       `json:"alpn,omitempty"`
	Reality          RealityShapeV1 `json:"reality"`
}

type ALPNFallbackShapeV1 struct {
	ALPN   string        `json:"alpn"`
	Target TargetShapeV1 `json:"target"`
}

type EffectiveInboundV1 struct {
	RuntimeAvailable    bool         `json:"runtimeAvailable"`
	Present             bool         `json:"present"`
	Type                string       `json:"type,omitempty"`
	Tag                 string       `json:"tag,omitempty"`
	Revision            string       `json:"revision,omitempty"`
	ConfigurationProven bool         `json:"configurationProven"`
	ReasonCodes         []ReasonCode `json:"reasonCodes,omitempty"`
}

type CapabilityDisposition string

const (
	CapabilitySupportedNaturalFallback CapabilityDisposition = "supported_natural_fallback"
	CapabilitySupported                CapabilityDisposition = "supported"
	CapabilityUnsupported              CapabilityDisposition = "unsupported"
	CapabilityNotShipped               CapabilityDisposition = "not_shipped"
	CapabilityOutOfScope               CapabilityDisposition = "out_of_scope"
	CapabilityUnknown                  CapabilityDisposition = "unknown"
)

type NativeFallbackVariant string

const (
	NativeFallbackNone                 NativeFallbackVariant = "none"
	NativeFallbackVLESSRealityTCP      NativeFallbackVariant = "vless_reality_handshake_tcp"
	NativeFallbackTrojanDefaultTCP     NativeFallbackVariant = "trojan_default_fallback_tcp"
	NativeFallbackTrojanALPNTCP        NativeFallbackVariant = "trojan_alpn_fallback_tcp"
	NativeFallbackTrojanDefaultALPNTCP NativeFallbackVariant = "trojan_default_and_alpn_fallback_tcp"
)

type NativeFallbackCapabilityV1 struct {
	Disposition                   CapabilityDisposition `json:"disposition"`
	Variant                       NativeFallbackVariant `json:"variant"`
	NaturalInvalidTrafficFallback bool                  `json:"naturalInvalidTrafficFallback"`
	ForcedSameSubjectDecoyRoute   bool                  `json:"forcedSameSubjectDecoyRoute"`
	CapabilityResolverRevision    string                `json:"capabilityResolverRevision"`
	ReasonCodes                   []ReasonCode          `json:"reasonCodes,omitempty"`
}

type CapabilityInputV1 struct {
	Runtime         CoreRuntimeIdentityV1 `json:"runtime"`
	ShapeKnown      bool                  `json:"shapeKnown"`
	InboundType     string                `json:"inboundType"`
	Listener        ListenerShapeV1       `json:"listener"`
	TLS             TLSShapeV1            `json:"tls"`
	Transport       TransportShapeV1      `json:"transport"`
	Multiplex       MultiplexShapeV1      `json:"multiplex"`
	DefaultFallback TargetShapeV1         `json:"defaultFallback"`
	ALPNFallbacks   []ALPNFallbackShapeV1 `json:"alpnFallbacks,omitempty"`
}

type InboundFallbackSnapshotV1 struct {
	Schema                     string                     `json:"schema"`
	InboundDatabaseID          uint                       `json:"inboundDatabaseId"`
	ResourceID                 string                     `json:"resourceId"`
	Tag                        string                     `json:"tag,omitempty"`
	Type                       string                     `json:"type"`
	Listener                   ListenerShapeV1            `json:"listener"`
	InboundOptionsDigest       string                     `json:"inboundOptionsDigest"`
	TLSRecordDatabaseID        uint                       `json:"tlsRecordDatabaseId,omitempty"`
	TLSResourceID              string                     `json:"tlsResourceId,omitempty"`
	TLSOptionsDigest           string                     `json:"tlsOptionsDigest,omitempty"`
	TLSReferenceCount          int64                      `json:"tlsReferenceCount"`
	TLS                        TLSShapeV1                 `json:"tls"`
	Transport                  TransportShapeV1           `json:"transport"`
	Multiplex                  MultiplexShapeV1           `json:"multiplex"`
	DefaultFallback            TargetShapeV1              `json:"defaultFallback"`
	ALPNFallbacks              []ALPNFallbackShapeV1      `json:"alpnFallbacks,omitempty"`
	Authentication             AuthenticationShapeV1      `json:"authentication"`
	LocalProxy                 LocalProxyShapeV1          `json:"localProxy"`
	Interception               InterceptionShapeV1        `json:"interception"`
	UDPTransport               UDPTransportShapeV2        `json:"udpTransport"`
	RuntimeIdentityRevision    string                     `json:"runtimeIdentityRevision"`
	CapabilityResolverRevision string                     `json:"capabilityResolverRevision"`
	Effective                  EffectiveInboundV1         `json:"effective"`
	ConfigurationRevision      string                     `json:"configurationRevision"`
	Capability                 NativeFallbackCapabilityV1 `json:"capability"`
	ReasonCodes                []ReasonCode               `json:"reasonCodes,omitempty"`
}
