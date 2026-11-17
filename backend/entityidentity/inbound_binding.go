package entityidentity

// InboundBinding is the Panel's credential-proven association between a rendered
// inbound principal and its durable VPN client. It carries no credential or
// runtime admission authority; the current Core owner still admits each flow.
type InboundBinding struct {
	Inbound   string
	Principal string
	ClientID  uint
}

// VPNUserID is the typed projection of the already proven local principal.
func (b InboundBinding) VPNUserID() VPNUserID { return VPNUserID(b.ClientID) }
