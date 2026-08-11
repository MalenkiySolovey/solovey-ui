package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"
)

const (
	ProtectionSignalSchemaV2   = "solovey-ui/protection-signal/v2"
	ProtectionDecisionSchemaV2 = "solovey-ui/protection-decision/v2"
	MaxSignalLifetime          = 24 * time.Hour
)

const (
	ReasonUnknown                  = "unknown"
	ReasonStale                    = "stale"
	ReasonTruncated                = "truncated"
	ReasonAmbiguous                = "ambiguous"
	ReasonCapabilityUnavailable    = "capability_unavailable"
	ReasonCompatibilityObserveOnly = "legacy_compatibility_observe_only"
)

type SignalCategory string

const (
	SignalCategoryEndpointObservation SignalCategory = "ENDPOINT_OBSERVATION"
	SignalCategoryConnectionMetadata  SignalCategory = "CONNECTION_METADATA"
	SignalCategoryKernelPressure      SignalCategory = "KERNEL_PRESSURE"
	SignalCategoryPanelAuth           SignalCategory = "PANEL_AUTH"
	SignalCategoryPanelAPI            SignalCategory = "PANEL_API"
	SignalCategorySubscription        SignalCategory = "SUBSCRIPTION"
	SignalCategorySSHAuth             SignalCategory = "SSH_AUTH"
	SignalCategoryHostSurface         SignalCategory = "HOST_SURFACE"
	SignalCategoryHostResource        SignalCategory = "HOST_RESOURCE"
	SignalCategoryExternalReputation  SignalCategory = "EXTERNAL_REPUTATION"
	SignalCategoryConfigDrift         SignalCategory = "CONFIG_DRIFT"
)

type DecisionScope string

const (
	ScopeEndpoint     DecisionScope = "ENDPOINT"
	ScopeService      DecisionScope = "SERVICE"
	ScopePanelAuth    DecisionScope = "PANEL_AUTH"
	ScopePanelAPI     DecisionScope = "PANEL_API"
	ScopeSSH          DecisionScope = "SSH"
	ScopeSubscription DecisionScope = "SUBSCRIPTION"
	ScopeHostWide     DecisionScope = "HOST_WIDE"
)

type SignalSourceV2 struct {
	SourceID        string `json:"sourceId"`
	Producer        string `json:"producer"`
	ProducerVersion string `json:"producerVersion"`
	TrustClass      string `json:"trustClass"`
	SourceClass     string `json:"sourceClass"`
}

type SignalSubjectV2 struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type SignalScopeV2 struct {
	Scope            DecisionScope `json:"scope"`
	TargetResourceID string        `json:"targetResourceId,omitempty"`
	EndpointID       string        `json:"endpointId,omitempty"`
	Transport        string        `json:"transport,omitempty"`
}

type SignalProvenanceV2 struct {
	AdapterID           string   `json:"adapterId"`
	IntegrationID       string   `json:"integrationId,omitempty"`
	ExternalIDHash      string   `json:"externalIdHash,omitempty"`
	SourceRevision      string   `json:"sourceRevision"`
	PolicyRevision      string   `json:"policyRevision"`
	ObservationWindowID string   `json:"observationWindowId,omitempty"`
	EvidenceRefIDs      []string `json:"evidenceRefIds,omitempty"`
}

type ProtectionSignalV2 struct {
	Schema       string             `json:"schema"`
	SignalID     string             `json:"signalId"`
	Source       SignalSourceV2     `json:"source"`
	Category     SignalCategory     `json:"category"`
	Kind         string             `json:"kind"`
	KnownKind    bool               `json:"knownKind"`
	Subject      SignalSubjectV2    `json:"subject"`
	Scope        SignalScopeV2      `json:"scope"`
	ObservedAt   time.Time          `json:"observedAt"`
	ExpiresAt    time.Time          `json:"expiresAt"`
	ConfidenceBP int                `json:"confidenceBp"`
	SafeMeta     map[string]string  `json:"safeMeta,omitempty"`
	Provenance   SignalProvenanceV2 `json:"provenance"`
	ReasonCodes  []string           `json:"reasonCodes,omitempty"`
}

func (s *ProtectionSignalV2) FinalizeID(producerEventID string) {
	payload, _ := json.Marshal(struct {
		Schema     string             `json:"schema"`
		Source     SignalSourceV2     `json:"source"`
		EventID    string             `json:"event_id"`
		Category   SignalCategory     `json:"category"`
		Kind       string             `json:"kind"`
		Subject    SignalSubjectV2    `json:"subject"`
		Scope      SignalScopeV2      `json:"scope"`
		ObservedAt time.Time          `json:"observed_at"`
		ExpiresAt  time.Time          `json:"expires_at"`
		Provenance SignalProvenanceV2 `json:"provenance"`
	}{s.Schema, s.Source, strings.TrimSpace(producerEventID), s.Category, s.Kind, s.Subject, s.Scope, s.ObservedAt.UTC(), s.ExpiresAt.UTC(), s.Provenance})
	sum := sha256.Sum256(payload)
	s.SignalID = hex.EncodeToString(sum[:])
}

func (s ProtectionSignalV2) Validate(now time.Time) error {
	if s.Schema != ProtectionSignalSchemaV2 || !ValidSHA256(s.SignalID) || !ValidContractID(s.Source.SourceID, 128) || !ValidContractID(s.Source.Producer, 128) || !ValidContractID(s.Source.ProducerVersion, 64) || !ValidContractID(s.Source.TrustClass, 64) {
		return errors.New("signal schema or identity is invalid")
	}
	if s.Source.SourceClass != "native" && s.Source.SourceClass != "external" {
		return errors.New("signal source class is invalid")
	}
	if !validSignalCategory(s.Category) || !ValidContractID(s.Kind, 128) || !validateScopeShape(s.Scope) || !SignalScopeAllowed(s.Category, s.Scope.Scope) {
		return errors.New("signal category, kind, or scope is invalid")
	}
	if s.KnownKind != SignalKindKnown(s.Category, s.Kind) {
		return errors.New("signal known-kind claim does not match the versioned enum")
	}
	if err := validateSubject(s.Subject); err != nil {
		return err
	}
	if s.ObservedAt.IsZero() || s.ExpiresAt.IsZero() || !s.ExpiresAt.After(s.ObservedAt) || s.ExpiresAt.After(s.ObservedAt.Add(MaxSignalLifetime)) || s.ObservedAt.After(now.UTC().Add(5*time.Minute)) {
		return errors.New("signal timestamps are invalid")
	}
	if s.ConfidenceBP < 0 || s.ConfidenceBP > 10000 {
		return errors.New("signal confidence is invalid")
	}
	if err := ValidateProtectionSignalSafeMeta(s.SafeMeta); err != nil {
		return err
	}
	if !ValidContractID(s.Provenance.AdapterID, 128) || (s.Provenance.IntegrationID != "" && !ValidContractID(s.Provenance.IntegrationID, 128)) || !ValidContractID(s.Provenance.SourceRevision, 128) || !ValidContractID(s.Provenance.PolicyRevision, 128) || (s.Provenance.ObservationWindowID != "" && !ValidContractID(s.Provenance.ObservationWindowID, 128)) || (s.Provenance.ExternalIDHash != "" && !ValidSHA256(s.Provenance.ExternalIDHash)) || len(s.Provenance.EvidenceRefIDs) > 16 || !validateReasonCodes(s.ReasonCodes) {
		return errors.New("signal provenance is invalid")
	}
	for _, ref := range s.Provenance.EvidenceRefIDs {
		if !ValidContractID(ref, 128) {
			return errors.New("signal evidence reference is invalid")
		}
	}
	if !sortedUniqueStrings(s.Provenance.EvidenceRefIDs) {
		return errors.New("signal evidence references are not canonical")
	}
	return nil
}

type ResponseIntent string

const (
	IntentObserve             ResponseIntent = "OBSERVE"
	IntentSoftGraylist        ResponseIntent = "SOFT_GRAYLIST"
	IntentRateLimit           ResponseIntent = "RATE_LIMIT"
	IntentRouteToDecoy        ResponseIntent = "ROUTE_TO_DECOY"
	IntentTemporaryQuarantine ResponseIntent = "TEMPORARY_QUARANTINE"
	IntentTemporaryBlock      ResponseIntent = "TEMPORARY_BLOCK"
	IntentManualHardBlock     ResponseIntent = "MANUAL_HARD_BLOCK"
)

type DecisionStateV2 string

const (
	DecisionCandidate  DecisionStateV2 = "CANDIDATE"
	DecisionResolved   DecisionStateV2 = "RESOLVED"
	DecisionApplied    DecisionStateV2 = "APPLIED"
	DecisionDegraded   DecisionStateV2 = "DEGRADED"
	DecisionFailed     DecisionStateV2 = "FAILED"
	DecisionExpired    DecisionStateV2 = "EXPIRED"
	DecisionSuperseded DecisionStateV2 = "SUPERSEDED"
)

type ScoreSnapshotV2 struct {
	Score       int       `json:"score"`
	TargetGroup string    `json:"targetGroup"`
	CapturedAt  time.Time `json:"capturedAt"`
}
type PolicyCheckV2 struct {
	Result      string   `json:"result"`
	ReasonCodes []string `json:"reasonCodes,omitempty"`
}
type DecisionRevisionBindingV2 struct {
	StrategyRevision      string `json:"strategyRevision"`
	CapabilityRevision    string `json:"capabilityRevision"`
	ActionScopeRevision   string `json:"actionScopeRevision"`
	EndpointRevision      string `json:"endpointRevision"`
	ResourceRevision      string `json:"resourceRevision"`
	ConfigurationRevision string `json:"configurationRevision"`
}
type CapabilityResolutionV2 struct {
	Implemented    bool           `json:"implemented"`
	ResolvedIntent ResponseIntent `json:"resolvedIntent"`
	ReasonCodes    []string       `json:"reasonCodes,omitempty"`
}

type ProtectionDecisionV2 struct {
	Schema                string                 `json:"schema"`
	DecisionID            string                 `json:"decisionId"`
	PolicyRevision        string                 `json:"policyRevision"`
	StrategyRevision      string                 `json:"strategyRevision,omitempty"`
	CapabilityRevision    string                 `json:"capabilityRevision,omitempty"`
	ActionScopeRevision   string                 `json:"actionScopeRevision,omitempty"`
	EndpointRevision      string                 `json:"endpointRevision,omitempty"`
	ResourceRevision      string                 `json:"resourceRevision,omitempty"`
	ConfigurationRevision string                 `json:"configurationRevision,omitempty"`
	Subject               SignalSubjectV2        `json:"subject"`
	Scope                 SignalScopeV2          `json:"scope"`
	TargetResourceIDs     []string               `json:"targetResourceIds"`
	SignalRefs            []string               `json:"signalRefs"`
	SourceClasses         []string               `json:"sourceClasses"`
	ScoreSnapshot         ScoreSnapshotV2        `json:"scoreSnapshot"`
	ConfidenceBP          int                    `json:"confidenceBp"`
	ReasonCodes           []string               `json:"reasonCodes"`
	RequestedIntent       ResponseIntent         `json:"requestedIntent"`
	CreatedAt             time.Time              `json:"createdAt"`
	ExpiresAt             time.Time              `json:"expiresAt"`
	AllowlistResult       PolicyCheckV2          `json:"allowlistResult"`
	RecoveryResult        PolicyCheckV2          `json:"recoveryResult"`
	CapabilityResolution  CapabilityResolutionV2 `json:"capabilityResolution"`
	State                 DecisionStateV2        `json:"state"`
}

func (d *ProtectionDecisionV2) FinalizeID() {
	payload, _ := json.Marshal(struct {
		Policy            string          `json:"policy"`
		Strategy          string          `json:"strategy"`
		Capability        string          `json:"capability"`
		ActionScope       string          `json:"action_scope"`
		Endpoint          string          `json:"endpoint"`
		Resource          string          `json:"resource"`
		Configuration     string          `json:"configuration"`
		Subject           SignalSubjectV2 `json:"subject"`
		Scope             SignalScopeV2   `json:"scope"`
		TargetResourceIDs []string        `json:"target_resource_ids"`
		Signals           []string        `json:"signals"`
		SourceClasses     []string        `json:"source_classes"`
		Score             ScoreSnapshotV2 `json:"score"`
		ConfidenceBP      int             `json:"confidence_bp"`
		RequestedIntent   ResponseIntent  `json:"requested_intent"`
		CreatedAt         time.Time       `json:"created_at"`
		ExpiresAt         time.Time       `json:"expires_at"`
		AllowlistResult   PolicyCheckV2   `json:"allowlist_result"`
		RecoveryResult    PolicyCheckV2   `json:"recovery_result"`
	}{d.PolicyRevision, d.StrategyRevision, d.CapabilityRevision, d.ActionScopeRevision, d.EndpointRevision, d.ResourceRevision, d.ConfigurationRevision, d.Subject, d.Scope, sortedStrings(d.TargetResourceIDs), sortedStrings(d.SignalRefs), sortedStrings(d.SourceClasses), d.ScoreSnapshot, d.ConfidenceBP, d.RequestedIntent, d.CreatedAt.UTC(), d.ExpiresAt.UTC(), canonicalPolicyCheck(d.AllowlistResult), canonicalPolicyCheck(d.RecoveryResult)})
	sum := sha256.Sum256(payload)
	d.DecisionID = hex.EncodeToString(sum[:])
}

func (d ProtectionDecisionV2) Validate(now time.Time) error {
	if d.Schema != ProtectionDecisionSchemaV2 || !ValidSHA256(d.DecisionID) || !ValidContractID(d.PolicyRevision, 128) || !validateScopeShape(d.Scope) {
		return errors.New("decision schema, identity, policy, or scope is invalid")
	}
	expected := d
	expected.FinalizeID()
	if expected.DecisionID != d.DecisionID {
		return errors.New("decision identity does not match its immutable policy facts")
	}
	if err := validateSubject(d.Subject); err != nil {
		return err
	}
	if d.CreatedAt.IsZero() || d.ExpiresAt.IsZero() || !d.ExpiresAt.After(d.CreatedAt) || d.ExpiresAt.After(d.CreatedAt.Add(24*time.Hour)) || d.CreatedAt.After(now.UTC().Add(5*time.Minute)) {
		return errors.New("decision timestamps are invalid")
	}
	if d.ConfidenceBP < 0 || d.ConfidenceBP > 10000 || len(d.SignalRefs) > 128 || len(d.TargetResourceIDs) > 32 || !validateReasonCodes(d.ReasonCodes) || len(d.SourceClasses) == 0 || len(d.SourceClasses) > 16 {
		return errors.New("decision bounds are invalid")
	}
	seenSignals := make(map[string]struct{}, len(d.SignalRefs))
	for _, ref := range d.SignalRefs {
		if !ValidSHA256(ref) {
			return errors.New("decision signal reference is invalid")
		}
		if _, ok := seenSignals[ref]; ok {
			return errors.New("decision signal reference is duplicated")
		}
		seenSignals[ref] = struct{}{}
	}
	seenTargets := make(map[string]struct{}, len(d.TargetResourceIDs))
	for _, target := range d.TargetResourceIDs {
		if !ValidContractID(target, maxContractIDBytes) || (d.Scope.TargetResourceID != "" && target != d.Scope.TargetResourceID) {
			return errors.New("decision target crosses its declared scope")
		}
		if _, ok := seenTargets[target]; ok {
			return errors.New("decision target is duplicated")
		}
		seenTargets[target] = struct{}{}
	}
	if d.Scope.TargetResourceID != "" && (len(d.TargetResourceIDs) != 1 || d.TargetResourceIDs[0] != d.Scope.TargetResourceID) {
		return errors.New("decision target does not match its declared scope")
	}
	if d.Scope.TargetResourceID == "" && len(d.TargetResourceIDs) != 0 {
		return errors.New("unscoped decision targets are invalid")
	}
	if d.Scope.EndpointID != "" {
		if !ValidContractID(d.StrategyRevision, 128) || !ValidContractID(d.CapabilityRevision, 128) ||
			!ValidExactRevision(d.ActionScopeRevision) || !ValidExactRevision(d.EndpointRevision) ||
			!ValidExactRevision(d.ResourceRevision) || !ValidExactRevision(d.ConfigurationRevision) {
			return errors.New("endpoint decision revision binding is incomplete")
		}
	}
	for _, sourceClass := range d.SourceClasses {
		if sourceClass != "native" && sourceClass != "external" {
			return errors.New("decision source class is invalid")
		}
	}
	expectedTargetGroup := d.Scope.TargetResourceID
	if expectedTargetGroup == "" {
		expectedTargetGroup = string(d.Scope.Scope)
	}
	if !ValidContractID(d.ScoreSnapshot.TargetGroup, maxContractIDBytes) || d.ScoreSnapshot.TargetGroup != expectedTargetGroup || d.ScoreSnapshot.CapturedAt.IsZero() || d.ScoreSnapshot.CapturedAt.Before(d.CreatedAt.Add(-5*time.Minute)) || d.ScoreSnapshot.CapturedAt.After(d.CreatedAt.Add(5*time.Minute)) {
		return errors.New("decision score snapshot is invalid")
	}
	if !validatePolicyCheck(d.AllowlistResult) || !validatePolicyCheck(d.RecoveryResult) || !validateReasonCodes(d.CapabilityResolution.ReasonCodes) {
		return errors.New("decision policy result is invalid")
	}
	if !validIntent(d.RequestedIntent) || !validIntent(d.CapabilityResolution.ResolvedIntent) {
		return errors.New("decision intent is invalid")
	}
	if d.State == DecisionApplied {
		return errors.New("a ProtectionDecisionV2 is never an AppliedActionV1")
	}
	if d.CapabilityResolution.Implemented {
		if d.CapabilityResolution.ResolvedIntent == IntentObserve || d.Scope.Scope != ScopeEndpoint || d.Scope.TargetResourceID == "" {
			return errors.New("implemented action capability requires a scoped non-observe endpoint intent")
		}
	} else if d.CapabilityResolution.ResolvedIntent != IntentObserve {
		return errors.New("an unavailable action capability must resolve to OBSERVE")
	}
	switch d.State {
	case DecisionCandidate, DecisionResolved, DecisionDegraded, DecisionFailed, DecisionExpired, DecisionSuperseded:
	default:
		return errors.New("decision state is invalid")
	}
	return nil
}

func validSignalCategory(value SignalCategory) bool {
	switch value {
	case SignalCategoryEndpointObservation, SignalCategoryConnectionMetadata, SignalCategoryKernelPressure, SignalCategoryPanelAuth, SignalCategoryPanelAPI, SignalCategorySubscription, SignalCategorySSHAuth, SignalCategoryHostSurface, SignalCategoryHostResource, SignalCategoryExternalReputation, SignalCategoryConfigDrift:
		return true
	}
	return false
}
func validScope(value DecisionScope) bool {
	switch value {
	case ScopeEndpoint, ScopeService, ScopePanelAuth, ScopePanelAPI, ScopeSSH, ScopeSubscription, ScopeHostWide:
		return true
	}
	return false
}
func validIntent(value ResponseIntent) bool {
	switch value {
	case IntentObserve, IntentSoftGraylist, IntentRateLimit, IntentRouteToDecoy, IntentTemporaryQuarantine, IntentTemporaryBlock, IntentManualHardBlock:
		return true
	}
	return false
}
func validateSubject(value SignalSubjectV2) error {
	switch value.Type {
	case "ip":
		address, err := netip.ParseAddr(value.Value)
		if err != nil || address.Unmap().String() != value.Value {
			return errors.New("signal IP subject is invalid or non-canonical")
		}
	case "prefix":
		prefix, err := netip.ParsePrefix(value.Value)
		if err != nil || prefix.Masked().String() != value.Value {
			return errors.New("signal prefix subject is invalid or non-canonical")
		}
	case "endpoint", "service":
		if !ValidContractID(value.Value, 256) {
			return errors.New("signal subject is invalid")
		}
	case "account_pseudonym":
		if !strings.HasPrefix(value.Value, "account:") || !ValidSHA256(strings.TrimPrefix(value.Value, "account:")) {
			return errors.New("signal account subject is not a bounded pseudonym")
		}
	default:
		return errors.New("signal subject type is invalid")
	}
	return nil
}
func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func canonicalPolicyCheck(value PolicyCheckV2) PolicyCheckV2 {
	value.ReasonCodes = sortedStrings(value.ReasonCodes)
	return value
}
