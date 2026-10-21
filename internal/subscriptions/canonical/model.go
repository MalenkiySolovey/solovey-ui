package canonical

const (
	FormatSingBox = "sing-box-json"
	FormatXray    = "xray-json"
	FormatURI     = "uri-list"
	FormatClash   = "clash-yaml"
	FormatUnknown = "unknown"
)

const MetadataKey = "_subscription"

const SnapshotVersion = 1

const (
	KindSingle = "single"
	KindGroup  = "group"

	RoleTopLevel = "top"
	RoleMember   = "member"
)

type Snapshot struct {
	Version     int           `json:"version"`
	Formats     []string      `json:"formats,omitempty"`
	Connections []Connection  `json:"connections,omitempty"`
	Extras      []Observation `json:"extras,omitempty"`
}

// EndpointIR is the existing canonical Connection model, evolved in place.
// Its native observations/preserved options remain in the current snapshot.
type EndpointIR struct {
	EndpointID     string                 `json:"endpointId,omitempty"`
	Authentication Authentication         `json:"authentication,omitempty"`
	Multiplex      map[string]any         `json:"multiplex,omitempty"`
	UDP            bool                   `json:"udp"`
	Projection     string                 `json:"projection,omitempty"`
	Diagnostics    []ProjectionDiagnostic `json:"diagnostics,omitempty"`
	Kind           string                 `json:"kind,omitempty"`
	Role           string                 `json:"role,omitempty"`
	DisplayName    string                 `json:"displayName,omitempty"`
	Protocol       string                 `json:"protocol,omitempty"`
	Endpoint       Endpoint               `json:"endpoint,omitempty"`
	TLS            TLS                    `json:"tls,omitempty"`
	Transport      Transport              `json:"transport,omitempty"`
	GroupMembers   []string               `json:"groupMembers,omitempty"`
	BestOutbound   map[string]any         `json:"bestOutbound,omitempty"`
	Formats        []string               `json:"formats,omitempty"`
	Adaptations    []Adaptation           `json:"adaptations,omitempty"`
	Observations   []Observation          `json:"observations,omitempty"`
}

// Connection is a compatibility name for the same IR, not a second model.
type Connection = EndpointIR

type Endpoint struct {
	Server string `json:"server,omitempty"`
	Port   string `json:"port,omitempty"`
}

type TLS struct {
	Options    map[string]any `json:"options,omitempty"`
	Enabled    bool           `json:"enabled,omitempty"`
	ServerName string         `json:"serverName,omitempty"`
	Reality    bool           `json:"reality,omitempty"`
}

type Transport struct {
	Options map[string]any `json:"options,omitempty"`
	Type    string         `json:"type,omitempty"`
	Path    string         `json:"path,omitempty"`
	Host    string         `json:"host,omitempty"`
}

type Observation struct {
	Source      map[string]any         `json:"source,omitempty"`
	Diagnostics []ProjectionDiagnostic `json:"diagnostics,omitempty"`
	Format      string                 `json:"format"`
	Name        string                 `json:"name,omitempty"`
	Outbound    map[string]any         `json:"outbound,omitempty"`
}

type Adaptation struct {
	SourceFormat  string `json:"sourceFormat,omitempty"`
	SourceFeature string `json:"sourceFeature,omitempty"`
	SourceType    string `json:"sourceType,omitempty"`
	TargetType    string `json:"targetType,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	Note          string `json:"note,omitempty"`
}
