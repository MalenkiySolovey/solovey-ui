package model

// InboundEndpointLease is the core-owned durable authority for an ordinary
// inbound endpoint consumed by a fronting or local-proxy guard operation.
// Components store only mirrors; deleting a mirror cannot mutate this row.
type InboundEndpointLease struct {
	LeaseID            string `gorm:"primaryKey;size:128"`
	InboundID          uint   `gorm:"not null;index"`
	ProviderID         string `gorm:"size:128;not null;index"`
	HolderID           string `gorm:"size:128;not null;index"`
	ResourceID         string `gorm:"size:256;not null;index"`
	EndpointID         string `gorm:"size:128;not null;index"`
	ExactReferenceJSON []byte `gorm:"not null"`
	LeaseJSON          []byte `gorm:"not null"`
	LeaseRevision      string `gorm:"size:64;not null"`
	State              string `gorm:"size:32;not null;index"`
	LastRequestID      string `gorm:"size:128;not null"`
	IssuedAtUnix       int64  `gorm:"not null;index"`
	RenewedAtUnix      int64  `gorm:"not null"`
	ExpiresAtUnix      int64  `gorm:"not null;index"`
	ReleasedAtUnix     int64  `gorm:"not null;default:0;index"`
}

func (InboundEndpointLease) TableName() string { return "inbound_endpoint_leases" }
