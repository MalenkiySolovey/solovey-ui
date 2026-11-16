package model

// InboundFallbackCheckpoint stores a bounded, redacted core-owned recovery
// payload. Payload is deliberately opaque outside coreinboundcontrol.
type InboundFallbackCheckpoint struct {
	ID                string `gorm:"primaryKey;size:64"`
	Schema            string `gorm:"size:96;not null"`
	InboundID         uint   `gorm:"not null;index"`
	Payload           []byte `gorm:"not null"`
	IntegrityDigest   string `gorm:"size:64;not null"`
	State             string `gorm:"size:32;not null;index"`
	AfterRevision     string `gorm:"size:64"`
	EffectiveRevision string `gorm:"size:64"`
	ProofDigest       string `gorm:"size:64"`
	CreatedAtUnix     int64  `gorm:"not null;index"`
	ExpiresAtUnix     int64  `gorm:"not null;index"`
	ReleasedAtUnix    int64  `gorm:"not null;default:0;index"`
}

func (InboundFallbackCheckpoint) TableName() string {
	return "inbound_fallback_checkpoints"
}
