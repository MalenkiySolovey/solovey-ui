package repository

import "encoding/json"

type SettingsModel struct {
	ID                         uint            `gorm:"primaryKey;autoIncrement:false"`
	Revision                   int             `gorm:"not null;default:1"`
	Enabled                    bool            `gorm:"not null"`
	RetentionGlobalLimit       int             `gorm:"not null"`
	RetentionPerResourceLimit  int             `gorm:"not null"`
	DefaultScoreThreshold      int             `gorm:"not null"`
	DefaultGraylistTTLSeconds  int             `gorm:"not null"`
	DiagnosticsCacheTTLSeconds int             `gorm:"not null"`
	ObservationBufferSize      int             `gorm:"not null"`
	ObservationFlushIntervalMS int             `gorm:"not null"`
	IPv6GraylistPrefixBits     int             `gorm:"not null"`
	MaxScore                   int             `gorm:"not null"`
	SafeMetaMaxBytes           int             `gorm:"not null"`
	ClockSkewToleranceSeconds  int             `gorm:"not null"`
	ArtifactRetentionCount     int             `gorm:"not null"`
	ArtifactRetentionDays      int             `gorm:"not null"`
	AdvancedAcknowledgedAt     int64           `gorm:"not null;default:0"`
	FeatureFlagsJSON           json.RawMessage `gorm:"column:feature_flags_json;not null"`
}

func (SettingsModel) TableName() string { return "server_protection_settings" }

type PortAllowlistModel struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	Protocol  string `gorm:"not null;uniqueIndex:idx_server_protection_port_allow"`
	Listen    string `gorm:"not null;uniqueIndex:idx_server_protection_port_allow"`
	PortStart int    `gorm:"not null;uniqueIndex:idx_server_protection_port_allow"`
	PortEnd   int    `gorm:"not null;uniqueIndex:idx_server_protection_port_allow"`
	Reason    string `gorm:"not null"`
	ExpiresAt *int64
	CreatedBy string `gorm:"not null"`
	CreatedAt int64  `gorm:"not null"`
	UpdatedAt int64  `gorm:"not null"`
}

func (PortAllowlistModel) TableName() string { return "server_protection_port_allowlist" }

type IPAllowlistModel struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	IPCIDR    string `gorm:"column:ip_cidr;not null;uniqueIndex"`
	Reason    string `gorm:"not null"`
	ExpiresAt *int64
	CreatedBy string `gorm:"not null"`
	CreatedAt int64  `gorm:"not null"`
	UpdatedAt int64  `gorm:"not null"`
}

func (IPAllowlistModel) TableName() string { return "server_protection_ip_allowlist" }

type ProfileModel struct {
	ID                  uint   `gorm:"primaryKey;autoIncrement"`
	ResourceID          string `gorm:"not null;uniqueIndex:idx_server_protection_profile_resource"`
	ResourceKind        string `gorm:"not null;uniqueIndex:idx_server_protection_profile_resource"`
	ResourceOwner       string `gorm:"not null;uniqueIndex:idx_server_protection_profile_resource"`
	InboundTag          string `gorm:"index"`
	Enabled             bool   `gorm:"not null"`
	Status              string `gorm:"not null"`
	Mode                string `gorm:"not null"`
	MigrationCandidate  string
	MigrationReason     string
	LegacyTopologyState string
	ResourceFingerprint string
	AcceptedFingerprint string
	LastSeenFingerprint string
	FallbackResourceID  string
	PublicListen        string
	PublicPort          *int
	HandshakeHost       string
	HandshakeTargetHost string
	HandshakeTargetPort *int
	ScoreThreshold      int    `gorm:"not null"`
	GraylistTTLSeconds  int    `gorm:"not null"`
	DefaultAction       string `gorm:"not null"`
	ManagedFirewall     bool   `gorm:"not null"`
	Revision            int    `gorm:"not null"`
	CreatedAt           int64  `gorm:"not null"`
	UpdatedAt           int64  `gorm:"not null"`
}

func (ProfileModel) TableName() string { return "server_protection_profiles" }

type GraylistModel struct {
	ID         uint `gorm:"primaryKey;autoIncrement"`
	ProfileID  *uint
	ResourceID string `gorm:"not null;uniqueIndex:idx_server_protection_graylist_resource"`
	IPCIDR     string `gorm:"column:ip_cidr;not null;uniqueIndex:idx_server_protection_graylist_resource"`
	IPFamily   int    `gorm:"not null"`
	Score      int    `gorm:"not null"`
	Reason     string `gorm:"not null"`
	LastSignal string `gorm:"not null"`
	ExpiresAt  int64  `gorm:"not null;index"`
	CreatedAt  int64  `gorm:"not null"`
	UpdatedAt  int64  `gorm:"not null"`
}

func (GraylistModel) TableName() string { return "server_protection_graylist" }

type GraylistStateV2Model struct {
	ID                 uint            `gorm:"primaryKey;autoIncrement"`
	StateID            string          `gorm:"column:state_id;not null;uniqueIndex"`
	Schema             string          `gorm:"not null"`
	Revision           uint64          `gorm:"not null"`
	SubjectType        string          `gorm:"not null"`
	SubjectValue       string          `gorm:"not null"`
	ResourceID         string          `gorm:"not null;index:idx_server_protection_graylist_v2_identity"`
	EndpointID         string          `gorm:"not null;index:idx_server_protection_graylist_v2_identity"`
	Transport          string          `gorm:"not null;index:idx_server_protection_graylist_v2_identity"`
	PolicyRevision     string          `gorm:"not null;index:idx_server_protection_graylist_v2_identity"`
	StrategyRevision   string          `gorm:"not null;index:idx_server_protection_graylist_v2_identity"`
	CapabilityRevision string          `gorm:"not null;index:idx_server_protection_graylist_v2_identity"`
	Score              int             `gorm:"not null"`
	ConfidenceBP       int             `gorm:"not null"`
	Band               string          `gorm:"not null;index"`
	Lifecycle          string          `gorm:"not null;index"`
	SelectedResponse   string          `gorm:"not null"`
	DesiredAction      string          `gorm:"not null"`
	ActualActionState  string          `gorm:"not null"`
	AppliedActionRefID string          `gorm:"not null;default:''"`
	EnteredAt          int64           `gorm:"not null"`
	LastSignalAt       int64           `gorm:"not null"`
	ExpiresAt          int64           `gorm:"not null;index"`
	CreatedAt          int64           `gorm:"not null"`
	UpdatedAt          int64           `gorm:"not null"`
	SignalRefCount     int             `gorm:"not null"`
	ReasonCount        int             `gorm:"not null"`
	ContractJSON       json.RawMessage `gorm:"column:contract_json;not null"`
}

func (GraylistStateV2Model) TableName() string { return "server_protection_graylist_v2" }

type ScoreStateModel struct {
	ID                      uint            `gorm:"primaryKey;autoIncrement"`
	ResourceID              string          `gorm:"not null;uniqueIndex:idx_server_protection_score_resource"`
	SourcePrefix            string          `gorm:"not null;uniqueIndex:idx_server_protection_score_resource"`
	IPFamily                int             `gorm:"not null"`
	CurrentScore            int             `gorm:"not null"`
	RawScore                int             `gorm:"not null"`
	FirstSeenAt             int64           `gorm:"not null"`
	LastSignalAt            int64           `gorm:"not null"`
	ExpiresAt               *int64          `gorm:"index"`
	ReasonsJSON             json.RawMessage `gorm:"column:reasons_json;not null"`
	LastDecision            string          `gorm:"not null"`
	DedupeJSON              json.RawMessage `gorm:"column:dedupe_json"`
	ClassifierPolicyVersion int             `gorm:"not null"`
	CreatedAt               int64           `gorm:"not null"`
	UpdatedAt               int64           `gorm:"not null"`
}

func (ScoreStateModel) TableName() string { return "server_protection_score_states" }

type ProbeEventModel struct {
	ID            uint `gorm:"primaryKey;autoIncrement"`
	ProfileID     *uint
	ResourceID    string `gorm:"not null;index"`
	ResourceKind  string `gorm:"not null"`
	SourceIPCIDR  string `gorm:"column:source_ip_cidr"`
	IPFamily      *int
	SignalKind    string          `gorm:"not null"`
	ScoreDelta    int             `gorm:"not null"`
	Action        string          `gorm:"not null"`
	SafeMetaJSON  json.RawMessage `gorm:"column:safe_meta_json;not null"`
	SafeMetaBytes int             `gorm:"not null"`
	ObservedAt    int64           `gorm:"not null;index"`
	DedupeKey     string          `gorm:"not null"`
}

func (ProbeEventModel) TableName() string { return "server_protection_probe_events" }

type ProtectionSignalV2Model struct {
	ID                  uint            `gorm:"primaryKey;autoIncrement"`
	SignalID            string          `gorm:"column:signal_id;not null;uniqueIndex"`
	Schema              string          `gorm:"not null"`
	SourceID            string          `gorm:"not null;index"`
	SourceClass         string          `gorm:"not null"`
	Category            string          `gorm:"not null;index"`
	Kind                string          `gorm:"not null;index"`
	KnownKind           bool            `gorm:"not null"`
	SubjectType         string          `gorm:"not null"`
	SubjectValue        string          `gorm:"not null"`
	Scope               string          `gorm:"not null;index"`
	TargetResourceID    string          `gorm:"index"`
	EndpointID          string          `gorm:"index"`
	Transport           string          `gorm:"index"`
	ObservationWindowID string          `gorm:"index"`
	ObservedAt          int64           `gorm:"not null;index"`
	ExpiresAt           int64           `gorm:"not null;index"`
	ConfidenceBP        int             `gorm:"not null"`
	PolicyRevision      string          `gorm:"not null"`
	SafeMetaBytes       int             `gorm:"not null"`
	ContractJSON        json.RawMessage `gorm:"column:contract_json;not null"`
}

func (ProtectionSignalV2Model) TableName() string { return "server_protection_signals_v2" }

type ProtectionDecisionV2Model struct {
	ID                uint            `gorm:"primaryKey;autoIncrement"`
	DecisionID        string          `gorm:"column:decision_id;not null;uniqueIndex"`
	Schema            string          `gorm:"not null"`
	PolicyRevision    string          `gorm:"not null;index"`
	SubjectType       string          `gorm:"not null"`
	SubjectValue      string          `gorm:"not null"`
	Scope             string          `gorm:"not null;index"`
	RequestedIntent   string          `gorm:"not null"`
	ResolvedIntent    string          `gorm:"not null"`
	ActionImplemented bool            `gorm:"not null;default:false"`
	State             string          `gorm:"not null;index"`
	CreatedAt         int64           `gorm:"not null;index"`
	ExpiresAt         int64           `gorm:"not null;index"`
	ContractJSON      json.RawMessage `gorm:"column:contract_json;not null"`
}

func (ProtectionDecisionV2Model) TableName() string { return "server_protection_decisions_v2" }

type PlannedResponseV2Model struct {
	ID                 uint            `gorm:"primaryKey;autoIncrement"`
	ResponseID         string          `gorm:"column:response_id;not null;uniqueIndex"`
	DecisionID         string          `gorm:"column:decision_id;not null;index"`
	ResourceID         string          `gorm:"not null;index"`
	EndpointID         string          `gorm:"not null;index"`
	SelectedIntent     string          `gorm:"not null"`
	CapabilityRevision string          `gorm:"not null;index"`
	ActualState        string          `gorm:"not null"`
	CreatedAt          int64           `gorm:"not null;index"`
	ExpiresAt          int64           `gorm:"not null;index"`
	ContractJSON       json.RawMessage `gorm:"column:contract_json;not null"`
}

func (PlannedResponseV2Model) TableName() string {
	return "server_protection_planned_responses_v2"
}

type FallbackTargetLeaseModel struct {
	ID                          uint            `gorm:"primaryKey;autoIncrement"`
	Schema                      string          `gorm:"not null;default:'solovey-ui/reference-lease/v1';size:96"`
	LeaseID                     string          `gorm:"column:lease_id;not null;uniqueIndex"`
	HolderID                    string          `gorm:"not null;index"`
	StrategyPlanID              string          `gorm:"size:64"`
	DecisionID                  string          `gorm:"size:128"`
	ActionID                    string          `gorm:"size:128"`
	OperationID                 string          `gorm:"size:128;index"`
	ResourceID                  string          `gorm:"size:256;index"`
	ProviderReservationID       string          `gorm:"size:128;index"`
	ProviderReservationRevision string          `gorm:"size:128"`
	ProviderID                  string          `gorm:"not null;index"`
	TargetID                    string          `gorm:"not null;index"`
	PublishRevision             string          `gorm:"not null"`
	ContentDigest               string          `gorm:"not null"`
	ApprovedLocalEndpointID     string          `gorm:"not null"`
	EndpointRevision            string          `gorm:"size:64"`
	ProviderHealthRevision      string          `gorm:"not null"`
	CapacityRevision            string          `gorm:"size:64"`
	ProviderRevision            string          `gorm:"size:128"`
	IssuedAt                    int64           `gorm:"not null"`
	RenewedAt                   int64           `gorm:"not null"`
	ExpiresAt                   int64           `gorm:"not null;index"`
	ReleasedAt                  int64           `gorm:"not null;default:0"`
	State                       string          `gorm:"not null;index"`
	ReasonCodesJSON             json.RawMessage `gorm:"column:reason_codes_json;not null"`
}

func (FallbackTargetLeaseModel) TableName() string { return "server_protection_fallback_target_leases" }

type RecoveryPathModel struct {
	ID                    uint   `gorm:"primaryKey;autoIncrement"`
	RecoveryPathID        string `gorm:"column:recovery_path_id;not null;uniqueIndex"`
	Kind                  string `gorm:"not null"`
	EndpointID            string `gorm:"not null;index"`
	PrincipalID           string `gorm:"not null"`
	SourcePrefix          string
	VerificationMethod    string          `gorm:"not null"`
	VerifiedAt            int64           `gorm:"not null"`
	ExpiresAt             int64           `gorm:"not null;index"`
	IndependenceClass     string          `gorm:"not null"`
	VerificationState     string          `gorm:"not null"`
	ReasonCodesJSON       json.RawMessage `gorm:"column:reason_codes_json;not null"`
	SourceRevision        string          `gorm:"column:source_revision;not null;default:''"`
	ConfigurationRevision string          `gorm:"column:configuration_revision;not null;default:''"`
	ProducerRevision      string          `gorm:"column:producer_revision;not null;default:''"`
}

func (RecoveryPathModel) TableName() string { return "server_protection_recovery_paths_v1" }

type PortOperationModel struct {
	ID                       uint            `gorm:"primaryKey;autoIncrement"`
	OperationID              string          `gorm:"not null;uniqueIndex"`
	IdempotencyKey           string          `gorm:"not null;uniqueIndex:idx_server_protection_port_operation_idempotency"`
	Revision                 int             `gorm:"not null;default:1"`
	State                    string          `gorm:"not null;index"`
	FromOwner                string          `gorm:"not null"`
	ToOwner                  string          `gorm:"not null"`
	Protocol                 string          `gorm:"not null"`
	Listen                   string          `gorm:"not null"`
	Port                     int             `gorm:"not null"`
	PreviousResourceJSON     json.RawMessage `gorm:"column:previous_resource_json;not null"`
	NextResourceJSON         json.RawMessage `gorm:"column:next_resource_json;not null"`
	PreviousResourceRevision string          `gorm:"not null"`
	NextResourceRevision     string          `gorm:"not null"`
	PreviousConfigRevision   string          `gorm:"not null"`
	NextConfigRevision       string          `gorm:"not null"`
	CoreConfigRevision       string
	FirewallRevision         string
	FallbackTargetRevision   string
	HealthResultJSON         json.RawMessage `gorm:"column:health_result_json"`
	HealthTargetsJSON        []byte          `gorm:"column:health_targets_json;not null;default:'[]'"`
	RollbackArtifactPath     string
	CreatedAt                int64 `gorm:"not null"`
	UpdatedAt                int64 `gorm:"not null"`
	PreparedAt               *int64
	AppliedAt                *int64
	RolledBackAt             *int64
}

func (PortOperationModel) TableName() string { return "server_protection_port_operations" }

type OperationLockModel struct {
	ID                 uint   `gorm:"primaryKey;autoIncrement"`
	OperationID        string `gorm:"not null;uniqueIndex"`
	Kind               string `gorm:"not null;index:idx_server_protection_lock_scope"`
	ResourceID         string
	Protocol           string `gorm:"index:idx_server_protection_lock_scope"`
	Listen             string `gorm:"index:idx_server_protection_lock_scope"`
	Port               *int   `gorm:"index:idx_server_protection_lock_scope"`
	State              string `gorm:"not null;index:idx_server_protection_lock_scope"`
	Revision           int    `gorm:"not null"`
	IdempotencyKey     string `gorm:"index:idx_server_protection_lock_idempotency"`
	PlanRevision       string `gorm:"column:plan_revision"`
	HelperRevision     string `gorm:"column:helper_revision"`
	LockedByPID        *int   `gorm:"column:locked_by_pid"`
	LockedByInstanceID string `gorm:"not null"`
	Actor              string `gorm:"not null"`
	RecoveryAttempts   int    `gorm:"not null;default:0"`
	LastRecoveryAt     *int64
	RecoveryErrorCode  string
	HeartbeatAt        int64 `gorm:"not null"`
	ExpiresAt          int64 `gorm:"not null"`
	CreatedAt          int64 `gorm:"not null"`
	UpdatedAt          int64 `gorm:"not null"`
}

func (OperationLockModel) TableName() string { return "server_protection_operation_locks" }

type ArtifactModel struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	OperationID    string `gorm:"not null;index:idx_server_protection_artifact_operation"`
	Revision       string `gorm:"not null;uniqueIndex:idx_server_protection_artifact_revision"`
	Scope          string `gorm:"not null"`
	RelativePath   string `gorm:"not null"`
	ManifestSHA256 string `gorm:"not null"`
	Bytes          int64  `gorm:"not null"`
	CreatedAt      int64  `gorm:"not null;index"`
	UpdatedAt      int64  `gorm:"not null"`
}

func (ArtifactModel) TableName() string { return "server_protection_artifacts" }

type FirewallStateModel struct {
	ID                   uint            `gorm:"primaryKey;autoIncrement"`
	Revision             string          `gorm:"not null;uniqueIndex"`
	Backend              string          `gorm:"not null"`
	State                string          `gorm:"not null"`
	AllowedPortsJSON     json.RawMessage `gorm:"column:allowed_ports_json;not null"`
	ManagedRulesJSON     json.RawMessage `gorm:"column:managed_rules_json;not null"`
	WarningsJSON         json.RawMessage `gorm:"column:warnings_json"`
	RollbackArtifactPath string
	CreatedAt            int64 `gorm:"not null"`
	AppliedAt            *int64
}

func (FirewallStateModel) TableName() string { return "server_protection_firewall_states" }

// FirewallContributionModel is the durable semantic authority for one
// independently owned part of the Solovey managed table. SemanticJSON is a
// bounded typed contract (baseline plan or exact UDP policy), never rendered
// nftables text.
type FirewallContributionModel struct {
	ContributionID     string          `gorm:"primaryKey;size:128"`
	Schema             string          `gorm:"not null;size:96"`
	Kind               string          `gorm:"not null;size:32;index"`
	ResourceID         string          `gorm:"not null;size:256;index"`
	EndpointID         string          `gorm:"size:128;index"`
	Network            string          `gorm:"not null;size:16;index"`
	AddressFamily      string          `gorm:"not null;size:16;index"`
	SemanticRevision   string          `gorm:"not null;size:64"`
	SemanticJSON       json.RawMessage `gorm:"column:semantic_json;not null"`
	AppliedOperationID string          `gorm:"not null;size:128;index"`
	CreatedAt          int64           `gorm:"not null"`
	UpdatedAt          int64           `gorm:"not null"`
}

func (FirewallContributionModel) TableName() string {
	return "server_protection_firewall_contributions_v1"
}

// FirewallCompositionModel records the exact deterministic aggregate that is
// currently authoritative. It contains contribution ids/revisions only; the
// complete candidate remains a derived artifact.
type FirewallCompositionModel struct {
	ID                  uint            `gorm:"primaryKey;autoIncrement:false"`
	Schema              string          `gorm:"not null;size:96"`
	Revision            string          `gorm:"not null;size:64"`
	ManagedPlanRevision string          `gorm:"not null;size:64"`
	CandidateSHA256     string          `gorm:"not null;size:64"`
	BindingsJSON        json.RawMessage `gorm:"column:bindings_json;not null"`
	State               string          `gorm:"not null;size:32;index"`
	AppliedOperationID  string          `gorm:"not null;size:128;index"`
	UpdatedAt           int64           `gorm:"not null"`
}

func (FirewallCompositionModel) TableName() string {
	return "server_protection_firewall_composition_v1"
}

// FirewallContributionTransitionModel fences one operation's contribution.
// Rollback restores PreviousJSON (or removes this contribution) and composes
// it with the then-current unrelated contributions.
type FirewallContributionTransitionModel struct {
	OperationID               string          `gorm:"primaryKey;size:128"`
	Schema                    string          `gorm:"not null;size:96"`
	ContributionID            string          `gorm:"not null;size:128;index"`
	PreviousPresent           bool            `gorm:"not null"`
	PreviousSemanticRevision  string          `gorm:"size:64"`
	PreviousJSON              json.RawMessage `gorm:"column:previous_json;not null"`
	DesiredSemanticRevision   string          `gorm:"not null;size:64"`
	DesiredJSON               json.RawMessage `gorm:"column:desired_json;not null"`
	BeforeCompositionRevision string          `gorm:"not null;size:64"`
	AfterCompositionRevision  string          `gorm:"not null;size:64"`
	ManagedPlanRevision       string          `gorm:"not null;size:64"`
	CandidateSHA256           string          `gorm:"not null;size:64"`
	State                     string          `gorm:"not null;size:32;index"`
	MarkerUnixNano            int64           `gorm:"not null;default:0"`
	MutationCompletedUnixNano int64           `gorm:"not null;default:0"`
	HealthProviderInstance    string          `gorm:"size:128"`
	HealthGeneration          uint64          `gorm:"not null;default:0"`
	HealthObservationRevision string          `gorm:"size:64"`
	HealthStartedUnixNano     int64           `gorm:"not null;default:0"`
	HealthCompletedUnixNano   int64           `gorm:"not null;default:0"`
	HealthExpiresUnixNano     int64           `gorm:"not null;default:0"`
	CreatedAt                 int64           `gorm:"not null"`
	UpdatedAt                 int64           `gorm:"not null"`
}

func (FirewallContributionTransitionModel) TableName() string {
	return "server_protection_firewall_contribution_transitions_v1"
}

type NativeFallbackOperationModel struct {
	ID                          uint            `gorm:"primaryKey;autoIncrement"`
	Schema                      string          `gorm:"not null;size:96"`
	OperationID                 string          `gorm:"not null;uniqueIndex;size:128"`
	Revision                    int             `gorm:"not null"`
	ResourceID                  string          `gorm:"not null;size:256;index"`
	InboundDatabaseID           uint            `gorm:"not null"`
	PlanID                      string          `gorm:"not null;size:64"`
	PlanDigest                  string          `gorm:"not null;size:64;index"`
	PlanJSON                    json.RawMessage `gorm:"column:plan_json;not null"`
	RuntimeIdentityRevision     string          `gorm:"not null;size:64"`
	CapabilityResolverRevision  string          `gorm:"not null;size:64"`
	BeforeConfigurationRevision string          `gorm:"not null;size:64"`
	ExpectedAfterRevision       string          `gorm:"not null;size:64"`
	AfterConfigurationRevision  string          `gorm:"size:64"`
	BeforeEffectiveRevision     string          `gorm:"not null;size:64"`
	ExpectedEffectiveRevision   string          `gorm:"size:64"`
	EffectiveRevision           string          `gorm:"size:64"`
	TargetReferenceJSON         json.RawMessage `gorm:"column:target_reference_json;not null"`
	TargetRevision              string          `gorm:"not null;size:64"`
	ProviderRevision            string          `gorm:"not null;size:128"`
	EndpointRevision            string          `gorm:"not null;size:64"`
	PublishRevision             string          `gorm:"not null;size:128"`
	HealthRevision              string          `gorm:"not null;size:64"`
	CapacityRevision            string          `gorm:"not null;size:64"`
	ProviderReservationID       string          `gorm:"size:128;index"`
	ProviderReservationRevision string          `gorm:"size:128"`
	CoreCheckpointID            string          `gorm:"size:128"`
	CoreCheckpointDigest        string          `gorm:"size:64"`
	CheckpointReleaseProof      string          `gorm:"size:64"`
	CoreCheckpointReleasedAt    *int64
	ArtifactRevision            string `gorm:"size:128"`
	ArtifactManifestDigest      string `gorm:"size:64"`
	MutationMarkedAt            *int64
	WorkflowState               string          `gorm:"not null;size:32;index"`
	HealthResultRevision        string          `gorm:"size:64"`
	HealthFactsJSON             json.RawMessage `gorm:"column:health_facts_json;not null"`
	ManagerGeneration           uint64          `gorm:"not null;default:0"`
	RollbackAttemptCount        int             `gorm:"not null;default:0"`
	RecoveryClassification      string          `gorm:"size:64"`
	ReasonCodesJSON             json.RawMessage `gorm:"column:reason_codes_json;not null"`
	RecoveryBundleJSON          json.RawMessage `gorm:"column:recovery_bundle_json;not null"`
	CreatedAt                   int64           `gorm:"not null"`
	UpdatedAt                   int64           `gorm:"not null"`
	PreparedAt                  *int64
	AppliedAt                   *int64
	RolledBackAt                *int64
}

func (NativeFallbackOperationModel) TableName() string {
	return "server_protection_native_fallback_operations"
}

type NativeFallbackStateModel struct {
	ResourceID                  string          `gorm:"primaryKey;size:256"`
	Schema                      string          `gorm:"not null;size:96"`
	InboundDatabaseID           uint            `gorm:"not null"`
	LatestPlanID                string          `gorm:"size:64"`
	LatestPlanDigest            string          `gorm:"size:64"`
	RuntimeIdentityRevision     string          `gorm:"size:64"`
	CapabilityResolverRevision  string          `gorm:"size:64"`
	BeforeConfigurationRevision string          `gorm:"size:64"`
	AfterConfigurationRevision  string          `gorm:"size:64"`
	EffectiveRevision           string          `gorm:"size:64"`
	TargetRevision              string          `gorm:"size:64"`
	ProviderRevision            string          `gorm:"size:128"`
	EndpointRevision            string          `gorm:"size:64"`
	PublishRevision             string          `gorm:"size:128"`
	HealthRevision              string          `gorm:"size:64"`
	CapacityRevision            string          `gorm:"size:64"`
	ProviderReservationID       string          `gorm:"size:128"`
	ProviderReservationRevision string          `gorm:"size:128"`
	OperationID                 string          `gorm:"size:128"`
	OperationRevision           string          `gorm:"size:128"`
	DesiredState                string          `gorm:"not null;size:32"`
	SelectedVariant             string          `gorm:"not null;size:64"`
	ActualState                 string          `gorm:"not null;size:32;index"`
	LastGoodCheckpointID        string          `gorm:"size:128"`
	LastGoodCheckpointDigest    string          `gorm:"size:64"`
	ReasonCodesJSON             json.RawMessage `gorm:"column:reason_codes_json;not null"`
	CreatedAt                   int64           `gorm:"not null"`
	UpdatedAt                   int64           `gorm:"not null"`
}

func (NativeFallbackStateModel) TableName() string {
	return "server_protection_native_fallback_states"
}

// FrontingStateV2Model is the bounded semantic projection owned by
// server-protection. It deliberately contains references and revisions only;
// candidate bytes, helper output, provider authority payloads and host-local
// paths remain in their existing authoritative stores.
type FrontingStateV2Model struct {
	ResourceID                 string          `gorm:"primaryKey;size:256"`
	Schema                     string          `gorm:"not null;size:96"`
	DisplayIdentity            string          `gorm:"size:256"`
	DesiredStrategy            string          `gorm:"not null;size:64"`
	SelectedStrategy           string          `gorm:"size:64"`
	ActualState                string          `gorm:"not null;size:32;index"`
	ApplyGate                  string          `gorm:"not null;size:64"`
	RuntimeState               string          `gorm:"not null;size:64"`
	InstallationClass          string          `gorm:"not null;size:64"`
	RuntimeIdentityRevision    string          `gorm:"size:64"`
	StrategyCapabilityRevision string          `gorm:"size:64"`
	SocketClaimJSON            json.RawMessage `gorm:"column:socket_claim_json;not null"`
	BackendReferencesJSON      json.RawMessage `gorm:"column:backend_references_json;not null"`
	FallbackReferencesJSON     json.RawMessage `gorm:"column:fallback_references_json;not null"`
	SelectorSetJSON            json.RawMessage `gorm:"column:selector_set_json;not null"`
	LeaseMirrorsJSON           json.RawMessage `gorm:"column:lease_mirrors_json;not null"`
	DefaultPolicy              string          `gorm:"size:64"`
	SelectedProxyMode          string          `gorm:"size:16"`
	ActiveMapRevision          string          `gorm:"size:64"`
	CandidateRevision          string          `gorm:"size:64"`
	ActiveRevision             string          `gorm:"size:64"`
	LatestOperationID          string          `gorm:"size:128;index"`
	LatestOperationRevision    int             `gorm:"not null;default:0"`
	LatestOperationState       string          `gorm:"size:32"`
	HealthState                string          `gorm:"size:32"`
	HealthObservedAt           int64
	HealthExpiresAt            int64
	RecoveryClassification     string          `gorm:"size:64"`
	CompatibilityState         string          `gorm:"not null;size:64;index"`
	ReasonCodesJSON            json.RawMessage `gorm:"column:reason_codes_json;not null"`
	BlocksJSON                 json.RawMessage `gorm:"column:blocks_json;not null"`
	WarningsJSON               json.RawMessage `gorm:"column:warnings_json;not null"`
	SafeNextAction             string          `gorm:"size:64"`
	GuardingProviderLease      bool            `gorm:"not null;default:false;index"`
	RecoverableArtifact        bool            `gorm:"not null;default:false;index"`
	OwnsActiveManagedRevision  bool            `gorm:"not null;default:false;index"`
	CreatedAt                  int64           `gorm:"not null"`
	UpdatedAt                  int64           `gorm:"not null"`
}

func (FrontingStateV2Model) TableName() string { return "server_protection_fronting_states_v2" }

// FrontingIdempotencyV2Model records only a digest and a bounded semantic
// response. A pending record is intentionally recovery-significant: replay
// cannot guess whether a mutation crossed its durable boundary.
type FrontingIdempotencyV2Model struct {
	ID                uint            `gorm:"primaryKey;autoIncrement"`
	Action            string          `gorm:"not null;size:16;uniqueIndex:idx_server_protection_fronting_receipt"`
	IdempotencyKey    string          `gorm:"not null;size:128;uniqueIndex:idx_server_protection_fronting_receipt"`
	RequestDigest     string          `gorm:"not null;size:64"`
	OperationID       string          `gorm:"size:128;index"`
	OperationRevision int             `gorm:"not null;default:0"`
	Status            string          `gorm:"not null;size:16;index"`
	ResponseJSON      json.RawMessage `gorm:"column:response_json;not null"`
	CreatedAt         int64           `gorm:"not null"`
	UpdatedAt         int64           `gorm:"not null"`
}

func (FrontingIdempotencyV2Model) TableName() string {
	return "server_protection_fronting_idempotency_v2"
}

type UDPGuardStateV1Model struct {
	ID                        uint   `gorm:"primaryKey;autoIncrement"`
	ResourceID                string `gorm:"not null;size:256;uniqueIndex:idx_server_protection_udp_guard_identity"`
	EndpointID                string `gorm:"not null;size:128;uniqueIndex:idx_server_protection_udp_guard_identity"`
	AddressFamily             string `gorm:"not null;size:16;default:'';index"`
	Schema                    string `gorm:"not null;size:64"`
	DesiredPolicy             string `gorm:"not null;size:32"`
	SelectedStrategy          string `gorm:"not null;size:32"`
	ActualState               string `gorm:"not null;size:32;index"`
	PlanID                    string `gorm:"not null;size:64"`
	PlanDigest                string `gorm:"not null;size:64"`
	CapabilityRevision        string `gorm:"not null;size:64"`
	ClaimRevision             string `gorm:"not null;size:64"`
	PolicyRevision            string `gorm:"not null;size:64"`
	ContributionID            string `gorm:"not null;size:128;index"`
	ContributionRevision      string `gorm:"not null;size:64"`
	CompositionRevision       string `gorm:"size:64"`
	ManagedPlanRevision       string `gorm:"size:64"`
	HealthProviderInstance    string `gorm:"size:128"`
	HealthGeneration          uint64 `gorm:"not null;default:0"`
	HealthObservationRevision string `gorm:"size:64"`
	HealthStartedUnixNano     int64  `gorm:"not null;default:0"`
	HealthCompletedUnixNano   int64  `gorm:"not null;default:0"`
	HealthExpiresUnixNano     int64  `gorm:"not null;default:0"`
	LatestOperationID         string `gorm:"size:128;index"`
	LatestOperationRevision   int    `gorm:"not null;default:0"`
	RecoveryRequired          bool   `gorm:"not null;default:false;index"`
	OwnsActiveContribution    bool   `gorm:"not null;default:false;index"`
	RecoverableArtifact       bool   `gorm:"not null;default:false;index"`
	CreatedAt                 int64  `gorm:"not null"`
	UpdatedAt                 int64  `gorm:"not null"`
}

func (UDPGuardStateV1Model) TableName() string { return "server_protection_udp_guard_states_v1" }

type UDPGuardIdempotencyV1Model struct {
	ID                   uint            `gorm:"primaryKey;autoIncrement"`
	Action               string          `gorm:"not null;size:16;uniqueIndex:idx_server_protection_udp_guard_receipt"`
	IdempotencyKey       string          `gorm:"not null;size:128;uniqueIndex:idx_server_protection_udp_guard_receipt"`
	RequestDigest        string          `gorm:"not null;size:64"`
	OperationID          string          `gorm:"size:128;index"`
	OperationRevision    int             `gorm:"not null;default:0"`
	Status               string          `gorm:"not null;size:16;index"`
	SemanticResponseJSON json.RawMessage `gorm:"column:semantic_response_json;not null"`
	CreatedAt            int64           `gorm:"not null"`
	UpdatedAt            int64           `gorm:"not null"`
}

func (UDPGuardIdempotencyV1Model) TableName() string {
	return "server_protection_udp_guard_idempotency_v1"
}

type LocalProxyStateV1Model struct {
	ID                      uint            `gorm:"primaryKey;autoIncrement"`
	ResourceID              string          `gorm:"not null;size:256;uniqueIndex:idx_server_protection_local_proxy_identity"`
	EndpointID              string          `gorm:"not null;size:128;uniqueIndex:idx_server_protection_local_proxy_identity"`
	Schema                  string          `gorm:"not null;size:96"`
	ActualState             string          `gorm:"not null;size:32;index"`
	ApplyGate               string          `gorm:"not null;size:64"`
	PlanID                  string          `gorm:"not null;size:96"`
	PlanDigest              string          `gorm:"not null;size:64"`
	PlanJSON                json.RawMessage `gorm:"column:plan_json;not null"`
	FactRevision            string          `gorm:"not null;size:64"`
	ReferenceRevision       string          `gorm:"not null;size:64"`
	LeaseID                 string          `gorm:"size:128;index"`
	LeaseRevision           string          `gorm:"size:64"`
	LeaseState              string          `gorm:"size:32"`
	LeaseRenewedAt          int64           `gorm:"not null;default:0"`
	LeaseExpiresAt          int64           `gorm:"not null;default:0"`
	LatestOperationID       string          `gorm:"size:128;index"`
	LatestOperationRevision int             `gorm:"not null;default:0"`
	MarkerRevision          string          `gorm:"size:64"`
	HealthJSON              json.RawMessage `gorm:"column:health_json;not null"`
	HealthRevision          string          `gorm:"size:64"`
	HealthExpiresUnixNano   int64           `gorm:"not null;default:0"`
	GuardingProviderLease   bool            `gorm:"not null;default:false;index"`
	RecoveryRequired        bool            `gorm:"not null;default:false;index"`
	CreatedAt               int64           `gorm:"not null"`
	UpdatedAt               int64           `gorm:"not null"`
}

func (LocalProxyStateV1Model) TableName() string {
	return "server_protection_local_proxy_states_v1"
}

type LocalProxyIdempotencyV1Model struct {
	ID                   uint            `gorm:"primaryKey;autoIncrement"`
	Action               string          `gorm:"not null;size:16;uniqueIndex:idx_server_protection_local_proxy_receipt"`
	IdempotencyKey       string          `gorm:"not null;size:128;uniqueIndex:idx_server_protection_local_proxy_receipt"`
	RequestDigest        string          `gorm:"not null;size:64"`
	OperationID          string          `gorm:"size:128;index"`
	OperationRevision    int             `gorm:"not null;default:0"`
	Status               string          `gorm:"not null;size:16;index"`
	SemanticResponseJSON json.RawMessage `gorm:"column:semantic_response_json;not null"`
	CreatedAt            int64           `gorm:"not null"`
	UpdatedAt            int64           `gorm:"not null"`
}

func (LocalProxyIdempotencyV1Model) TableName() string {
	return "server_protection_local_proxy_idempotency_v1"
}

type TableModel struct {
	Name  string
	Model any
}

func TableModels() []TableModel {
	return []TableModel{
		{"server_protection_settings", &SettingsModel{}},
		{"server_protection_port_allowlist", &PortAllowlistModel{}},
		{"server_protection_ip_allowlist", &IPAllowlistModel{}},
		{"server_protection_profiles", &ProfileModel{}},
		{"server_protection_graylist", &GraylistModel{}},
		{"server_protection_graylist_v2", &GraylistStateV2Model{}},
		{"server_protection_score_states", &ScoreStateModel{}},
		{"server_protection_probe_events", &ProbeEventModel{}},
		{"server_protection_signals_v2", &ProtectionSignalV2Model{}},
		{"server_protection_decisions_v2", &ProtectionDecisionV2Model{}},
		{"server_protection_planned_responses_v2", &PlannedResponseV2Model{}},
		{"server_protection_fallback_target_leases", &FallbackTargetLeaseModel{}},
		{"server_protection_recovery_paths_v1", &RecoveryPathModel{}},
		{"server_protection_port_operations", &PortOperationModel{}},
		{"server_protection_operation_locks", &OperationLockModel{}},
		{"server_protection_artifacts", &ArtifactModel{}},
		{"server_protection_firewall_states", &FirewallStateModel{}},
		{"server_protection_firewall_contributions_v1", &FirewallContributionModel{}},
		{"server_protection_firewall_composition_v1", &FirewallCompositionModel{}},
		{"server_protection_firewall_contribution_transitions_v1", &FirewallContributionTransitionModel{}},
		{"server_protection_native_fallback_operations", &NativeFallbackOperationModel{}},
		{"server_protection_native_fallback_states", &NativeFallbackStateModel{}},
		{"server_protection_fronting_states_v2", &FrontingStateV2Model{}},
		{"server_protection_fronting_idempotency_v2", &FrontingIdempotencyV2Model{}},
		{"server_protection_udp_guard_states_v1", &UDPGuardStateV1Model{}},
		{"server_protection_udp_guard_idempotency_v1", &UDPGuardIdempotencyV1Model{}},
		{"server_protection_local_proxy_states_v1", &LocalProxyStateV1Model{}},
		{"server_protection_local_proxy_idempotency_v1", &LocalProxyIdempotencyV1Model{}},
	}
}

// BackupTableModels excludes host-local rollback artifact metadata. Recovery
// files and their paths are meaningful only on the host that created them.
func BackupTableModels() []TableModel {
	all := TableModels()
	result := make([]TableModel, 0, len(all)-1)
	for _, table := range all {
		if table.Name != "server_protection_artifacts" {
			result = append(result, table)
		}
	}
	return result
}
