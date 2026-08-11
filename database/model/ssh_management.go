package model

// SSHPostureSnapshot stores a bounded, redacted semantic posture document.
// It never contains raw sshd configuration, file paths, command output,
// usernames, keys or credentials.
type SSHPostureSnapshot struct {
	ID               uint64 `json:"-" gorm:"primaryKey;autoIncrement"`
	SemanticRevision string `json:"semanticRevision" gorm:"column:semantic_revision;size:64;not null;uniqueIndex"`
	PayloadJSON      []byte `json:"payload" gorm:"column:payload_json;type:blob;not null"`
	ObservedAt       int64  `json:"observedAt" gorm:"column:observed_at;not null;index"`
	ExpiresAt        int64  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	CreatedAt        int64  `json:"createdAt" gorm:"column:created_at;not null"`
}

func (SSHPostureSnapshot) TableName() string { return "ssh_posture_snapshots_v1" }

// SSHManagementCandidate is the durable workflow authority. Candidate bytes
// are rendered deterministically from PolicyJSON and are not persisted here.
type SSHManagementCandidate struct {
	OperationID           string `json:"operationId" gorm:"column:operation_id;primaryKey;size:64"`
	Scope                 string `json:"-" gorm:"size:16;not null;default:global;index"`
	IdempotencyKey        string `json:"-" gorm:"column:idempotency_key;size:96;not null;uniqueIndex"`
	State                 string `json:"state" gorm:"size:40;not null;index"`
	Revision              uint64 `json:"revision" gorm:"not null"`
	PolicyJSON            []byte `json:"policy" gorm:"column:policy_json;type:blob;not null"`
	PreservationJSON      []byte `json:"preservation" gorm:"column:preservation_json;type:blob;not null"`
	CandidateDigest       string `json:"candidateDigest" gorm:"column:candidate_digest;size:64;not null"`
	BindingDigest         string `json:"bindingDigest" gorm:"column:binding_digest;size:64;not null"`
	BeforeArtifactDigest  string `json:"beforeArtifactDigest" gorm:"column:before_artifact_digest;size:64"`
	AfterArtifactDigest   string `json:"afterArtifactDigest" gorm:"column:after_artifact_digest;size:64"`
	PostureRevision       string `json:"postureRevision" gorm:"column:posture_revision;size:64;not null"`
	EndpointRevision      string `json:"endpointRevision" gorm:"column:endpoint_revision;size:64;not null"`
	RecoveryRevision      string `json:"recoveryRevision" gorm:"column:recovery_revision;size:64;not null"`
	ProviderRevision      string `json:"providerRevision" gorm:"column:provider_revision;size:64;not null"`
	BinaryRevision        string `json:"binaryRevision" gorm:"column:binary_revision;size:64;not null"`
	ServiceRevision       string `json:"serviceRevision" gorm:"column:service_revision;size:64;not null"`
	ConfigurationRevision string `json:"configurationRevision" gorm:"column:configuration_revision;size:64;not null"`
	EarliestSafetyExpiry  int64  `json:"earliestSafetyExpiry" gorm:"column:earliest_safety_expiry;not null;index"`
	ReconnectExpiresAt    int64  `json:"reconnectExpiresAt" gorm:"column:reconnect_expires_at;default:0;not null;index"`
	RollbackAttempts      uint8  `json:"rollbackAttempts" gorm:"column:rollback_attempts;default:0;not null"`
	RestoredUntrusted     bool   `json:"restoredUntrusted" gorm:"column:restored_untrusted;default:false;not null;index"`
	ReconciledAt          int64  `json:"reconciledAt" gorm:"column:reconciled_at;default:0;not null;index"`
	ReasonCodesJSON       []byte `json:"reasonCodes" gorm:"column:reason_codes_json;type:blob;not null"`
	CreatedAt             int64  `json:"createdAt" gorm:"column:created_at;not null"`
	UpdatedAt             int64  `json:"updatedAt" gorm:"column:updated_at;not null;index"`
}

func (SSHManagementCandidate) TableName() string { return "ssh_management_candidates_v1" }

// SSHManagedArtifactCheckpoint is deliberately excluded from backups. It is
// the exact local rollback checkpoint for the one managed drop-in only.
type SSHManagedArtifactCheckpoint struct {
	OperationID                 string `json:"-" gorm:"column:operation_id;primaryKey;size:64"`
	PriorPresent                bool   `json:"-" gorm:"column:prior_present;not null"`
	PriorContent                []byte `json:"-" gorm:"column:prior_content;type:blob"`
	PriorOwner                  string `json:"-" gorm:"column:prior_owner;size:32;not null"`
	PriorGroup                  string `json:"-" gorm:"column:prior_group;size:32;not null"`
	PriorModeClass              string `json:"-" gorm:"column:prior_mode_class;size:32;not null"`
	PriorMode                   uint32 `json:"-" gorm:"column:prior_mode;default:0;not null"`
	PriorDigest                 string `json:"-" gorm:"column:prior_digest;size:64;not null"`
	StagedArtifactDigest        string `json:"-" gorm:"column:staged_artifact_digest;size:64;not null"`
	StagedConfigurationRevision string `json:"-" gorm:"column:staged_configuration_revision;size:64;not null"`
	CreatedAt                   int64  `json:"-" gorm:"column:created_at;not null"`
}

func (SSHManagedArtifactCheckpoint) TableName() string { return "ssh_managed_artifact_checkpoints_v1" }

// SSHReconnectChallenge stores only a verifier digest and bounded bindings.
// It is deliberately excluded from backups.
type SSHReconnectChallenge struct {
	OperationID           string `json:"-" gorm:"column:operation_id;primaryKey;size:64"`
	CandidateDigest       string `json:"-" gorm:"column:candidate_digest;size:64;not null"`
	MarkerDigest          string `json:"-" gorm:"column:marker_digest;size:64;not null;uniqueIndex"`
	EndpointID            string `json:"-" gorm:"column:endpoint_id;size:256;not null"`
	PrincipalID           string `json:"-" gorm:"column:principal_id;size:256;not null"`
	AuthenticationClass   string `json:"-" gorm:"column:authentication_class;size:32;not null"`
	ServiceRevision       string `json:"-" gorm:"column:service_revision;size:64;not null"`
	BinaryRevision        string `json:"-" gorm:"column:binary_revision;size:64;not null"`
	ConfigurationRevision string `json:"-" gorm:"column:configuration_revision;size:64;not null"`
	VerifierDigest        string `json:"-" gorm:"column:verifier_digest;size:64;not null"`
	IssuedAt              int64  `json:"-" gorm:"column:issued_at;not null"`
	ExpiresAt             int64  `json:"-" gorm:"column:expires_at;not null;index"`
	ConsumedAt            int64  `json:"-" gorm:"column:consumed_at;default:0;not null;index"`
	Revision              uint64 `json:"-" gorm:"not null"`
}

func (SSHReconnectChallenge) TableName() string { return "ssh_reconnect_challenges_v1" }

type SSHRecoveryEvidence struct {
	ID                    string `json:"id" gorm:"primaryKey;size:256"`
	Kind                  string `json:"kind" gorm:"size:32;not null;index"`
	EndpointID            string `json:"endpointId" gorm:"column:endpoint_id;size:256;not null;index"`
	PrincipalID           string `json:"principalId" gorm:"column:principal_id;size:256;not null"`
	SourcePrefix          string `json:"sourcePrefix" gorm:"column:source_prefix;size:96"`
	VerificationMethod    string `json:"verificationMethod" gorm:"column:verification_method;size:64;not null"`
	EvidenceProvider      string `json:"evidenceProvider" gorm:"column:evidence_provider;size:128"`
	TargetOperation       string `json:"targetOperation" gorm:"column:target_operation;size:64;index"`
	VerifiedAt            int64  `json:"verifiedAt" gorm:"column:verified_at;not null"`
	ExpiresAt             int64  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	IndependenceClass     string `json:"independenceClass" gorm:"column:independence_class;size:64;not null"`
	VerificationState     string `json:"verificationState" gorm:"column:verification_state;size:32;not null;index"`
	OperationBound        bool   `json:"operationBound" gorm:"column:operation_bound;not null"`
	SingleUse             bool   `json:"singleUse" gorm:"column:single_use;not null"`
	ConsumedAt            int64  `json:"consumedAt" gorm:"column:consumed_at;default:0;not null"`
	Revision              uint64 `json:"revision" gorm:"not null"`
	ReasonCodesJSON       []byte `json:"reasonCodes" gorm:"column:reason_codes_json;type:blob;not null"`
	SourceRevision        string `json:"sourceRevision" gorm:"column:source_revision;size:64;not null"`
	ConfigurationRevision string `json:"configurationRevision" gorm:"column:configuration_revision;size:64;not null"`
	ServiceRevision       string `json:"serviceRevision" gorm:"column:service_revision;size:64"`
	BinaryRevision        string `json:"binaryRevision" gorm:"column:binary_revision;size:64"`
	ProducerRevision      string `json:"producerRevision" gorm:"column:producer_revision;size:64;not null"`
	UpdatedAt             int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (SSHRecoveryEvidence) TableName() string { return "ssh_recovery_evidence_v1" }

type SSHManagementJournal struct {
	ID          uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	OperationID string `json:"operationId" gorm:"column:operation_id;size:64;not null;index;uniqueIndex:idx_ssh_journal_operation_sequence"`
	Sequence    uint64 `json:"sequence" gorm:"not null;uniqueIndex:idx_ssh_journal_operation_sequence"`
	State       string `json:"state" gorm:"size:40;not null;index"`
	Event       string `json:"event" gorm:"size:64;not null"`
	ReasonCode  string `json:"reasonCode" gorm:"column:reason_code;size:64"`
	Revision    string `json:"revision" gorm:"size:64;not null"`
	CreatedAt   int64  `json:"createdAt" gorm:"column:created_at;not null;index"`
}

func (SSHManagementJournal) TableName() string { return "ssh_management_journal_v1" }
