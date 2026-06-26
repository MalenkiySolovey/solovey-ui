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

type Connection struct {
	Kind         string         `json:"kind,omitempty"`
	Role         string         `json:"role,omitempty"`
	DisplayName  string         `json:"displayName,omitempty"`
	Protocol     string         `json:"protocol,omitempty"`
	Endpoint     Endpoint       `json:"endpoint,omitempty"`
	TLS          TLS            `json:"tls,omitempty"`
	Transport    Transport      `json:"transport,omitempty"`
	GroupMembers []string       `json:"groupMembers,omitempty"`
	BestOutbound map[string]any `json:"bestOutbound,omitempty"`
	Formats      []string       `json:"formats,omitempty"`
	Adaptations  []Adaptation   `json:"adaptations,omitempty"`
	Observations []Observation  `json:"observations,omitempty"`
}

type Endpoint struct {
	Server string `json:"server,omitempty"`
	Port   string `json:"port,omitempty"`
}

type TLS struct {
	Enabled    bool   `json:"enabled,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	Reality    bool   `json:"reality,omitempty"`
}

type Transport struct {
	Type string `json:"type,omitempty"`
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

type Observation struct {
	Format   string         `json:"format"`
	Name     string         `json:"name,omitempty"`
	Outbound map[string]any `json:"outbound,omitempty"`
}

type Adaptation struct {
	SourceFormat  string `json:"sourceFormat,omitempty"`
	SourceFeature string `json:"sourceFeature,omitempty"`
	SourceType    string `json:"sourceType,omitempty"`
	TargetType    string `json:"targetType,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	Note          string `json:"note,omitempty"`
}
