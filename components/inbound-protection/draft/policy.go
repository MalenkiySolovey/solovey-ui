//go:build inbound_protection_draft

package draft

type Mode string
type ResourceKind string

const (
	ModeSelfSteal     Mode = "self_steal"
	ModeFrontedTLS    Mode = "fronted_tls"
	ModeFrontedStream Mode = "fronted_stream"
	ModeMetadataOnly  Mode = "metadata_only"
)

const (
	ResourcePanelListener ResourceKind = "panel_listener"
	ResourceInbound       ResourceKind = "inbound"
	ResourcePublicSite    ResourceKind = "public_site"
	ResourceNodeControl   ResourceKind = "node_control"
	ResourceComponent     ResourceKind = "component_listener"
)

type Profile struct {
	ID                    uint
	ResourceKind          ResourceKind
	ResourceID            string
	InboundTag            string
	Enabled               bool
	Mode                  Mode
	FallbackSiteID        uint
	PublicListen          string
	PublicPort            int
	HandshakeHost         string
	HandshakeTargetHost   string
	HandshakeTargetPort   int
	ScoreThreshold        int
	GraylistTTLSeconds    int
	OpenPorts             []int
	ManagedFirewallPolicy string
}

type GraylistEntry struct {
	ProfileID  uint
	IP         string
	Score      int
	Reason     string
	LastSignal string
	ExpiresAt  int64
}

type PortOwnershipTransfer struct {
	OperationID   string
	InboundTag    string
	FromOwner     string
	ToOwner       string
	PublicListen  string
	PublicPort    int
	LocalFallback string
	PreparedAt    int64
	AppliedAt     int64
	RolledBackAt  int64
}
