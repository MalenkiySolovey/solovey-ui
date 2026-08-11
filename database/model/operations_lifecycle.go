package model

// UpdateReleaseState stores only semantic release identity. It deliberately
// excludes source URLs, public-key material, artifacts and broker payloads.
type UpdateReleaseState struct {
	Channel              string `json:"channel" gorm:"primaryKey;size:16"`
	LastObservedSequence uint64 `json:"lastObservedSequence" gorm:"column:last_observed_sequence;not null;default:0"`
	LastVerifiedSequence uint64 `json:"lastVerifiedSequence" gorm:"column:last_verified_sequence;not null;default:0"`
	LastAppliedSequence  uint64 `json:"lastAppliedSequence" gorm:"column:last_applied_sequence;not null;default:0"`
	ReleaseID            string `json:"releaseId" gorm:"column:release_id;size:96;not null"`
	ManifestDigest       string `json:"manifestDigest" gorm:"column:manifest_digest;size:64;not null"`
	Version              string `json:"version" gorm:"size:96;not null"`
	SigningKeyID         string `json:"signingKeyId" gorm:"column:signing_key_id;size:96;not null"`
	ExpiresAt            int64  `json:"expiresAt" gorm:"column:expires_at;not null"`
	UpdatedAt            int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (UpdateReleaseState) TableName() string { return "update_release_state_v1" }

type UpdateOperation struct {
	OperationID        string `json:"operationId" gorm:"column:operation_id;primaryKey;size:96"`
	IdempotencyKey     string `json:"-" gorm:"column:idempotency_key;size:96;not null;uniqueIndex"`
	State              string `json:"state" gorm:"size:32;not null;index"`
	Channel            string `json:"channel" gorm:"size:16;not null"`
	Sequence           uint64 `json:"sequence" gorm:"not null"`
	ReleaseID          string `json:"releaseId" gorm:"column:release_id;size:96;not null"`
	Version            string `json:"version" gorm:"size:96;not null"`
	ManifestDigest     string `json:"manifestDigest" gorm:"column:manifest_digest;size:64;not null"`
	ArtifactSetDigest  string `json:"artifactSetDigest" gorm:"column:artifact_set_digest;size:64;not null"`
	Platform           string `json:"platform" gorm:"size:24;not null"`
	Arch               string `json:"arch" gorm:"size:24;not null"`
	BinaryProfile      string `json:"binaryProfile" gorm:"column:binary_profile;size:16;not null"`
	DeploymentRevision string `json:"deploymentRevision" gorm:"column:deployment_revision;size:64;not null"`
	BrokerCapability   string `json:"brokerCapability" gorm:"column:broker_capability;size:96;not null"`
	MigrationSetDigest string `json:"migrationSetDigest" gorm:"column:migration_set_digest;size:64;not null"`
	BackupRef          string `json:"backupRef" gorm:"column:backup_ref;size:64;not null"`
	RestartClass       string `json:"restartClass" gorm:"column:restart_class;size:24;not null"`
	RebootClass        string `json:"rebootClass" gorm:"column:reboot_class;size:24;not null"`
	RollbackClass      string `json:"rollbackClass" gorm:"column:rollback_class;size:24;not null"`
	BytesCompleted     int64  `json:"bytesCompleted" gorm:"column:bytes_completed;not null;default:0"`
	BytesTotal         int64  `json:"bytesTotal" gorm:"column:bytes_total;not null;default:0"`
	ReasonCode         string `json:"reasonCode" gorm:"column:reason_code;size:96;not null"`
	Revision           uint64 `json:"revision" gorm:"not null;default:1"`
	RestoredUntrusted  bool   `json:"restoredUntrusted" gorm:"column:restored_untrusted;not null;default:false;index"`
	RollbackAvailable  bool   `json:"rollbackAvailable" gorm:"column:rollback_available;not null;default:false"`
	CreatedAt          int64  `json:"createdAt" gorm:"column:created_at;not null"`
	UpdatedAt          int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (UpdateOperation) TableName() string { return "update_operations_v1" }

type UpdateJournal struct {
	Sequence     uint64 `json:"sequence" gorm:"primaryKey;autoIncrement"`
	OperationID  string `json:"operationId" gorm:"column:operation_id;size:96;not null;index"`
	State        string `json:"state" gorm:"size:32;not null"`
	Event        string `json:"event" gorm:"size:96;not null"`
	ReasonCode   string `json:"reasonCode" gorm:"column:reason_code;size:96;not null"`
	Revision     uint64 `json:"revision" gorm:"not null"`
	SemanticHash string `json:"semanticHash" gorm:"column:semantic_hash;size:64;not null"`
	CreatedAt    int64  `json:"createdAt" gorm:"column:created_at;not null;index"`
}

func (UpdateJournal) TableName() string { return "update_journal_v1" }

type ResourcePressureState struct {
	Scope             string `json:"-" gorm:"primaryKey;size:16"`
	State             string `json:"state" gorm:"size:24;not null;index"`
	PreviousState     string `json:"previousState" gorm:"column:previous_state;size:24;not null"`
	ReasonCode        string `json:"reasonCode" gorm:"column:reason_code;size:96;not null"`
	ObservationDigest string `json:"observationDigest" gorm:"column:observation_digest;size:64;not null"`
	Revision          uint64 `json:"revision" gorm:"not null;default:1"`
	Consecutive       uint32 `json:"consecutive" gorm:"not null;default:0"`
	ObservedAt        int64  `json:"observedAt" gorm:"column:observed_at;not null"`
	UpdatedAt         int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (ResourcePressureState) TableName() string { return "resource_pressure_state_v1" }

type ResourcePressureTransition struct {
	Sequence          uint64 `json:"sequence" gorm:"primaryKey;autoIncrement"`
	FromState         string `json:"fromState" gorm:"column:from_state;size:24;not null"`
	ToState           string `json:"toState" gorm:"column:to_state;size:24;not null;index"`
	ReasonCode        string `json:"reasonCode" gorm:"column:reason_code;size:96;not null"`
	ObservationDigest string `json:"observationDigest" gorm:"column:observation_digest;size:64;not null"`
	Revision          uint64 `json:"revision" gorm:"not null"`
	CreatedAt         int64  `json:"createdAt" gorm:"column:created_at;not null;index"`
}

func (ResourcePressureTransition) TableName() string { return "resource_pressure_transitions_v1" }

type MigrationJournal struct {
	Scope              string `json:"scope" gorm:"primaryKey;size:24"`
	OwnerID            string `json:"ownerId" gorm:"column:owner_id;primaryKey;size:96"`
	StepID             string `json:"stepId" gorm:"column:step_id;primaryKey;size:96"`
	Checksum           string `json:"checksum" gorm:"size:64;not null"`
	State              string `json:"state" gorm:"size:32;not null;index"`
	CompatibilityState string `json:"compatibilityState" gorm:"column:compatibility_state;size:32;not null"`
	RetryCount         uint32 `json:"retryCount" gorm:"column:retry_count;not null;default:0"`
	ErrorCode          string `json:"errorCode" gorm:"column:error_code;size:96;not null"`
	BackupRef          string `json:"backupRef" gorm:"column:backup_ref;size:96;not null"`
	RestoreRef         string `json:"restoreRef" gorm:"column:restore_ref;size:96;not null"`
	DropState          string `json:"dropState" gorm:"column:drop_state;size:32;not null"`
	StartedAt          int64  `json:"startedAt" gorm:"column:started_at;not null"`
	FinishedAt         int64  `json:"finishedAt" gorm:"column:finished_at;not null"`
	UpdatedAt          int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (MigrationJournal) TableName() string { return "migration_journal_v1" }

type DataLifecycleOperation struct {
	OperationID       string `json:"operationId" gorm:"column:operation_id;primaryKey;size:96"`
	IdempotencyKey    string `json:"-" gorm:"column:idempotency_key;size:96;not null;uniqueIndex"`
	Kind              string `json:"kind" gorm:"size:24;not null;index"`
	State             string `json:"state" gorm:"size:32;not null;index"`
	OwnerID           string `json:"ownerId" gorm:"column:owner_id;size:96;not null;index"`
	ManifestDigest    string `json:"manifestDigest" gorm:"column:manifest_digest;size:64;not null"`
	BackupRef         string `json:"backupRef" gorm:"column:backup_ref;size:64;not null"`
	ExpectedRevision  string `json:"expectedRevision" gorm:"column:expected_revision;size:64;not null"`
	ReasonCode        string `json:"reasonCode" gorm:"column:reason_code;size:96;not null"`
	Revision          uint64 `json:"revision" gorm:"not null;default:1"`
	RestoredUntrusted bool   `json:"restoredUntrusted" gorm:"column:restored_untrusted;not null;default:false;index"`
	CreatedAt         int64  `json:"createdAt" gorm:"column:created_at;not null"`
	UpdatedAt         int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (DataLifecycleOperation) TableName() string { return "data_lifecycle_operations_v1" }

type DataLifecycleJournal struct {
	Sequence    uint64 `json:"sequence" gorm:"primaryKey;autoIncrement"`
	OperationID string `json:"operationId" gorm:"column:operation_id;size:96;not null;index"`
	State       string `json:"state" gorm:"size:32;not null"`
	Event       string `json:"event" gorm:"size:96;not null"`
	ReasonCode  string `json:"reasonCode" gorm:"column:reason_code;size:96;not null"`
	Revision    uint64 `json:"revision" gorm:"not null"`
	CreatedAt   int64  `json:"createdAt" gorm:"column:created_at;not null;index"`
}

func (DataLifecycleJournal) TableName() string { return "data_lifecycle_journal_v1" }
