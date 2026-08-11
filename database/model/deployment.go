package model

type DeploymentState struct {
	Scope              string `json:"-" gorm:"primaryKey;size:16"`
	ProfileID          string `json:"profile" gorm:"column:profile_id;size:64;not null;index"`
	DesiredProfile     string `json:"desiredProfile" gorm:"column:desired_profile;size:64;not null"`
	GeneratedProfile   string `json:"generatedProfile" gorm:"column:generated_profile;size:64;not null"`
	GeneratedRevision  string `json:"generatedRevision" gorm:"column:generated_revision;size:64;not null"`
	InstalledProfile   string `json:"installedProfile" gorm:"column:installed_profile;size:64;not null"`
	ActiveProfile      string `json:"activeProfile" gorm:"column:active_profile;size:64;not null"`
	VerifiedProfile    string `json:"verifiedProfile" gorm:"column:verified_profile;size:64;not null"`
	CompatibilityState string `json:"compatibilityState" gorm:"column:compatibility_state;size:32;not null"`
	DoctorRevision     string `json:"doctorRevision" gorm:"column:doctor_revision;size:64;not null"`
	Runtime            string `json:"runtime" gorm:"size:16;not null"`
	PostureRevision    string `json:"postureRevision" gorm:"column:posture_revision;size:64;not null"`
	Trusted            bool   `json:"trusted" gorm:"not null;default:false;index"`
	ObservedAt         int64  `json:"observedAt" gorm:"column:observed_at;not null"`
	UpdatedAt          int64  `json:"updatedAt" gorm:"column:updated_at;not null"`
}

func (DeploymentState) TableName() string { return "deployment_state_v1" }

type DeploymentOperation struct {
	OperationID        string `json:"operationId" gorm:"column:operation_id;primaryKey;size:96"`
	IdempotencyKey     string `json:"-" gorm:"column:idempotency_key;size:96;not null;uniqueIndex"`
	State              string `json:"state" gorm:"size:40;not null;index"`
	FromProfile        string `json:"fromProfile" gorm:"column:from_profile;size:64;not null"`
	TargetProfile      string `json:"targetProfile" gorm:"column:target_profile;size:64;not null"`
	ExpectedPosture    string `json:"expectedPosture" gorm:"column:expected_posture;size:64;not null"`
	ExpectedManagement string `json:"expectedManagement" gorm:"column:expected_management;size:64;not null"`
	CheckpointRef      string `json:"-" gorm:"column:checkpoint_ref;size:64"`
	BrokerReceipt      string `json:"brokerReceipt" gorm:"column:broker_receipt;size:64"`
	Revision           uint64 `json:"revision" gorm:"not null"`
	RestoredUntrusted  bool   `json:"restoredUntrusted" gorm:"column:restored_untrusted;not null;default:false;index"`
	ReconciledAt       int64  `json:"reconciledAt" gorm:"column:reconciled_at;not null;default:0"`
	CreatedAt          int64  `json:"createdAt" gorm:"column:created_at;not null"`
	UpdatedAt          int64  `json:"updatedAt" gorm:"column:updated_at;not null;index"`
	ReasonsJSON        []byte `json:"reasons" gorm:"column:reasons_json;type:blob;not null"`
	BindingRevision    string `json:"bindingRevision" gorm:"column:binding_revision;size:64;not null"`
}

func (DeploymentOperation) TableName() string { return "deployment_operations_v1" }

type DeploymentJournal struct {
	ID          uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	OperationID string `json:"operationId" gorm:"column:operation_id;size:96;not null;uniqueIndex:idx_deployment_journal_operation_sequence"`
	Sequence    uint64 `json:"sequence" gorm:"not null;uniqueIndex:idx_deployment_journal_operation_sequence"`
	State       string `json:"state" gorm:"size:40;not null;index"`
	Event       string `json:"event" gorm:"size:64;not null"`
	Reason      string `json:"reason" gorm:"size:64"`
	Revision    string `json:"revision" gorm:"size:64;not null"`
	CreatedAt   int64  `json:"createdAt" gorm:"column:created_at;not null;index"`
}

func (DeploymentJournal) TableName() string { return "deployment_journal_v1" }

type DeploymentDoctorSnapshot struct {
	ID          uint64 `json:"-" gorm:"primaryKey;autoIncrement"`
	Revision    string `json:"revision" gorm:"size:64;not null;uniqueIndex"`
	ProfileID   string `json:"profile" gorm:"column:profile_id;size:64;not null;index"`
	Healthy     bool   `json:"healthy" gorm:"not null;index"`
	PayloadJSON []byte `json:"payload" gorm:"column:payload_json;type:blob;not null"`
	GeneratedAt int64  `json:"generatedAt" gorm:"column:generated_at;not null;index"`
}

func (DeploymentDoctorSnapshot) TableName() string { return "deployment_doctor_snapshots_v1" }
