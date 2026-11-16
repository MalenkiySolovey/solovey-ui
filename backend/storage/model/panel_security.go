package model

// AdminMFAFactor stores one administrator-owned TOTP lifecycle. Ciphertext is
// never serialized and remains bound to the product secretbox authority.
type AdminMFAFactor struct {
	UserID                    uint   `json:"userId" gorm:"primaryKey;not null"`
	State                     string `json:"state" gorm:"size:32;not null;index"`
	PendingSecretCiphertext   string `json:"-" gorm:"column:pending_secret_ciphertext;type:text"`
	PendingExpiresAt          int64  `json:"pendingExpiresAt" gorm:"column:pending_expires_at;default:0;not null;index"`
	PendingAcceptedCounter    int64  `json:"-" gorm:"column:pending_accepted_counter;default:-1;not null"`
	PendingRecoveryGeneration uint64 `json:"-" gorm:"column:pending_recovery_generation;default:0;not null"`
	ActiveSecretCiphertext    string `json:"-" gorm:"column:active_secret_ciphertext;type:text"`
	LastAcceptedCounter       int64  `json:"-" gorm:"column:last_accepted_counter;default:-1;not null"`
	RecoveryGeneration        uint64 `json:"-" gorm:"column:recovery_generation;default:0;not null"`
	RecoveryAcknowledged      bool   `json:"recoveryAcknowledged" gorm:"column:recovery_acknowledged;default:false;not null"`
	UpdatedAt                 int64  `json:"updatedAt" gorm:"column:updated_at;default:0;not null"`
	User                      User   `json:"-" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

type AdminRecoveryCode struct {
	ID         uint   `json:"-" gorm:"primaryKey;autoIncrement"`
	UserID     uint   `json:"-" gorm:"not null;uniqueIndex:idx_admin_recovery_code"`
	Generation uint64 `json:"-" gorm:"not null;uniqueIndex:idx_admin_recovery_code"`
	Verifier   string `json:"-" gorm:"size:64;not null;uniqueIndex:idx_admin_recovery_code"`
	CreatedAt  int64  `json:"-" gorm:"column:created_at;not null"`
	UsedAt     int64  `json:"-" gorm:"column:used_at;default:0;not null;index"`
	User       User   `json:"-" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

// SecuritySession is metadata for the existing sessions row. SessionID is a
// private foreign key; Ref is the only identifier exposed to clients.
type SecuritySession struct {
	SessionID                 string `json:"-" gorm:"column:session_id;primaryKey;size:128"`
	Ref                       string `json:"ref" gorm:"size:64;not null;uniqueIndex"`
	UserID                    uint   `json:"-" gorm:"column:user_id;not null;index"`
	UsernameSnapshot          string `json:"username" gorm:"column:username_snapshot;size:256;not null"`
	State                     string `json:"state" gorm:"size:32;not null;index"`
	AuthState                 string `json:"authState" gorm:"column:auth_state;size:32;not null;index"`
	Assurance                 string `json:"assurance" gorm:"size:32;not null"`
	LastMFAAt                 int64  `json:"lastMfaAt" gorm:"column:last_mfa_at;default:0;not null"`
	LifetimePosture           string `json:"lifetimePosture" gorm:"column:lifetime_posture;size:32;not null"`
	SessionGenerationRevision string `json:"-" gorm:"column:session_generation_revision;size:64;not null;index"`
	CredentialGeneration      uint64 `json:"-" gorm:"column:credential_generation;not null"`
	MFAGeneration             uint64 `json:"-" gorm:"column:mfa_generation;not null"`
	CreatedAt                 int64  `json:"createdAt" gorm:"column:created_at;not null"`
	AuthenticatedAt           int64  `json:"authenticatedAt" gorm:"column:authenticated_at;not null"`
	LastSeenAt                int64  `json:"lastSeenAt" gorm:"column:last_seen_at;not null;index"`
	IdleExpiresAt             int64  `json:"idleExpiresAt" gorm:"column:idle_expires_at;default:0;not null;index"`
	AbsoluteExpiresAt         int64  `json:"absoluteExpiresAt" gorm:"column:absolute_expires_at;default:0;not null;index"`
	RememberedExpiresAt       int64  `json:"rememberedExpiresAt" gorm:"column:remembered_expires_at;default:0;not null"`
	ClientProvenance          string `json:"clientProvenance" gorm:"column:client_provenance;size:32;not null"`
	ClientPrefix              string `json:"clientPrefix" gorm:"column:client_prefix;size:96;not null"`
	UserAgentHash             string `json:"-" gorm:"column:user_agent_hash;size:64;not null"`
	DeviceLabel               string `json:"deviceLabel" gorm:"column:device_label;size:96;not null"`
	RevokedAt                 int64  `json:"revokedAt" gorm:"column:revoked_at;default:0;not null;index"`
	RevokedReason             string `json:"revokedReason" gorm:"column:revoked_reason;size:64;not null"`
	ReplacementRef            string `json:"replacementRef,omitempty" gorm:"column:replacement_ref;size:64"`
	User                      User   `json:"-" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

type StepUpGrant struct {
	Digest                    string `json:"-" gorm:"primaryKey;size:64"`
	Revision                  string `json:"revision" gorm:"size:64;not null;uniqueIndex"`
	UserID                    uint   `json:"-" gorm:"column:user_id;not null;index"`
	SessionRef                string `json:"-" gorm:"column:session_ref;size:64;not null;index"`
	SessionGenerationRevision string `json:"-" gorm:"column:session_generation_revision;size:64;not null"`
	CredentialGeneration      uint64 `json:"-" gorm:"column:credential_generation;not null"`
	MFAGeneration             uint64 `json:"-" gorm:"column:mfa_generation;not null"`
	OperationKind             string `json:"operationKind" gorm:"column:operation_kind;size:96;not null;index"`
	// TargetDigest is the server-derived composite of the caller-supplied
	// operation target and the issuing request's client-identity revision.
	TargetDigest string `json:"targetDigest" gorm:"column:target_digest;size:64;not null"`
	Assurance    string `json:"assurance" gorm:"size:32;not null"`
	CreatedAt    int64  `json:"createdAt" gorm:"column:created_at;not null"`
	ExpiresAt    int64  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	ConsumedAt   int64  `json:"consumedAt" gorm:"column:consumed_at;default:0;not null"`
	User         User   `json:"-" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}
