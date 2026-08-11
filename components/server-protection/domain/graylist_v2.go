package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	GraylistStateSchemaV2            = "solovey-ui/graylist-state/v2"
	StrategyActionCapabilitySchemaV2 = "solovey-ui/strategy-action-capability/v2"
	PlannedResponseSchemaV2          = "solovey-ui/planned-response/v2"
	MaxGraylistSignalRefs            = 64
)

type GraylistBandV2 string

const (
	GraylistBandObserve       GraylistBandV2 = "OBSERVE"
	GraylistBandGraylist      GraylistBandV2 = "GRAYLIST"
	GraylistBandRateCandidate GraylistBandV2 = "RATE_CANDIDATE"
	GraylistBandCooldown      GraylistBandV2 = "COOLDOWN"
)

type GraylistLifecycleV2 string

const (
	GraylistLifecycleActive      GraylistLifecycleV2 = "ACTIVE"
	GraylistLifecycleCooldown    GraylistLifecycleV2 = "COOLDOWN"
	GraylistLifecycleExpired     GraylistLifecycleV2 = "EXPIRED"
	GraylistLifecycleSuperseded  GraylistLifecycleV2 = "SUPERSEDED"
	GraylistLifecycleLegacyStale GraylistLifecycleV2 = "LEGACY_STALE"
)

type GraylistEvidenceClassV2 string

const (
	GraylistEvidenceStrongTrusted GraylistEvidenceClassV2 = "STRONG_TRUSTED"
	GraylistEvidenceWeak          GraylistEvidenceClassV2 = "WEAK"
	GraylistEvidenceExternal      GraylistEvidenceClassV2 = "EXTERNAL"
)

type GraylistEvidenceRefV2 struct {
	SignalID  string                  `json:"signalId"`
	Class     GraylistEvidenceClassV2 `json:"class"`
	ExpiresAt time.Time               `json:"expiresAt"`
}

// GraylistStateV2 is policy state, never executor evidence. StateID identifies
// the exact subject/scope/policy/strategy/capability tuple.
type GraylistStateV2 struct {
	Schema             string                  `json:"schema"`
	StateID            string                  `json:"stateId"`
	Revision           uint64                  `json:"revision"`
	Subject            SignalSubjectV2         `json:"subject"`
	ResourceID         string                  `json:"resourceId"`
	EndpointID         string                  `json:"endpointId"`
	Transport          string                  `json:"transport"`
	Score              int                     `json:"score"`
	ConfidenceBP       int                     `json:"confidenceBp"`
	PolicyRevision     string                  `json:"policyRevision"`
	StrategyRevision   string                  `json:"strategyRevision"`
	CapabilityRevision string                  `json:"capabilityRevision"`
	SignalRefs         []string                `json:"signalRefs"`
	EvidenceRefs       []GraylistEvidenceRefV2 `json:"evidenceRefs,omitempty"`
	SourceClasses      []string                `json:"sourceClasses"`
	Band               GraylistBandV2          `json:"band"`
	Lifecycle          GraylistLifecycleV2     `json:"lifecycle"`
	EnteredAt          time.Time               `json:"enteredAt"`
	LastSignalAt       time.Time               `json:"lastSignalAt"`
	ExpiresAt          time.Time               `json:"expiresAt"`
	SelectedResponse   ResponseIntent          `json:"selectedResponse"`
	DesiredAction      ResponseIntent          `json:"desiredAction"`
	ActualActionState  string                  `json:"actualActionState"`
	AppliedActionRefID string                  `json:"appliedActionRefId,omitempty"`
	ReasonCodes        []string                `json:"reasonCodes"`
	CreatedAt          time.Time               `json:"createdAt"`
	UpdatedAt          time.Time               `json:"updatedAt"`
}

func (state *GraylistStateV2) FinalizeID() {
	payload, _ := json.Marshal(struct {
		Subject            SignalSubjectV2 `json:"subject"`
		ResourceID         string          `json:"resource_id"`
		EndpointID         string          `json:"endpoint_id"`
		Transport          string          `json:"transport"`
		PolicyRevision     string          `json:"policy_revision"`
		StrategyRevision   string          `json:"strategy_revision"`
		CapabilityRevision string          `json:"capability_revision"`
	}{state.Subject, state.ResourceID, state.EndpointID, state.Transport, state.PolicyRevision, state.StrategyRevision, state.CapabilityRevision})
	sum := sha256.Sum256(payload)
	state.StateID = hex.EncodeToString(sum[:])
}

func (state GraylistStateV2) Validate() error {
	if state.Schema != GraylistStateSchemaV2 || !ValidSHA256(state.StateID) || state.Revision == 0 ||
		!ValidContractID(state.ResourceID, 256) || !ValidContractID(state.EndpointID, 256) ||
		(state.Transport != "tcp" && state.Transport != "udp") ||
		!ValidContractID(state.PolicyRevision, 128) || !ValidContractID(state.StrategyRevision, 128) ||
		!ValidContractID(state.CapabilityRevision, 128) {
		return errors.New("graylist identity or revision is invalid")
	}
	expected := state
	expected.FinalizeID()
	if expected.StateID != state.StateID {
		return errors.New("graylist identity does not match its semantic key")
	}
	if err := validateSubject(state.Subject); err != nil {
		return err
	}
	if state.Score < 0 || state.Score > 100 || state.ConfidenceBP < 0 || state.ConfidenceBP > 10000 ||
		len(state.SignalRefs) > MaxGraylistSignalRefs || len(state.SourceClasses) == 0 || len(state.SourceClasses) > 2 || !validateReasonCodes(state.ReasonCodes) {
		return errors.New("graylist bounds are invalid")
	}
	if !sortedUniqueSHA256(state.SignalRefs) || !sortedUniqueStrings(state.SourceClasses) || !sortedUniqueStrings(state.ReasonCodes) {
		return errors.New("graylist references or reasons are not canonical")
	}
	if len(state.EvidenceRefs) > MaxGraylistSignalRefs {
		return errors.New("graylist evidence exceeds its bound")
	}
	for index, evidence := range state.EvidenceRefs {
		if !ValidSHA256(evidence.SignalID) || evidence.ExpiresAt.IsZero() ||
			(evidence.Class != GraylistEvidenceStrongTrusted && evidence.Class != GraylistEvidenceWeak && evidence.Class != GraylistEvidenceExternal) ||
			(index > 0 && state.EvidenceRefs[index-1].SignalID >= evidence.SignalID) {
			return errors.New("graylist evidence is invalid or non-canonical")
		}
	}
	for _, sourceClass := range state.SourceClasses {
		if sourceClass != "external" && sourceClass != "native" {
			return errors.New("graylist source class is invalid")
		}
	}
	if state.CreatedAt.IsZero() || state.EnteredAt.IsZero() || state.LastSignalAt.IsZero() ||
		state.UpdatedAt.IsZero() || state.ExpiresAt.IsZero() ||
		state.EnteredAt.Before(state.CreatedAt) || state.LastSignalAt.Before(state.EnteredAt) ||
		state.UpdatedAt.Before(state.LastSignalAt) || !state.ExpiresAt.After(state.EnteredAt) ||
		state.ExpiresAt.After(state.EnteredAt.Add(24*time.Hour)) {
		return errors.New("graylist timestamps are invalid")
	}
	if !validIntent(state.SelectedResponse) || !validIntent(state.DesiredAction) {
		return errors.New("graylist response is invalid")
	}
	switch state.Band {
	case GraylistBandObserve, GraylistBandGraylist, GraylistBandRateCandidate, GraylistBandCooldown:
	default:
		return errors.New("graylist band is invalid")
	}
	switch state.Lifecycle {
	case GraylistLifecycleActive, GraylistLifecycleCooldown, GraylistLifecycleExpired, GraylistLifecycleSuperseded, GraylistLifecycleLegacyStale:
	default:
		return errors.New("graylist lifecycle is invalid")
	}
	if state.Band == GraylistBandCooldown && state.Lifecycle != GraylistLifecycleCooldown && state.Lifecycle != GraylistLifecycleExpired && state.Lifecycle != GraylistLifecycleSuperseded {
		return errors.New("graylist cooldown band has an incompatible lifecycle")
	}
	switch state.ActualActionState {
	case "NOT_APPLIED", "APPLIED", "EXPIRED", "ROLLED_BACK":
	default:
		return errors.New("graylist actual action state is invalid")
	}
	if state.ActualActionState == "APPLIED" {
		if !ValidSHA256(state.AppliedActionRefID) {
			return errors.New("graylist APPLIED projection lacks exact executor evidence")
		}
	} else if state.AppliedActionRefID != "" {
		return errors.New("graylist non-applied projection carries applied evidence")
	}
	return nil
}

type StrategyActionCapabilityV2 struct {
	Schema                        string    `json:"schema"`
	Strategy                      string    `json:"strategy"`
	ActionRevision                string    `json:"actionRevision"`
	StrategyRevision              string    `json:"strategyRevision"`
	CapabilityRevision            string    `json:"capabilityRevision"`
	ResourceID                    string    `json:"resourceId"`
	EndpointID                    string    `json:"endpointId"`
	ActionScopeRevision           string    `json:"actionScopeRevision"`
	EndpointRevision              string    `json:"endpointRevision"`
	ResourceRevision              string    `json:"resourceRevision"`
	ConfigurationRevision         string    `json:"configurationRevision"`
	NaturalInvalidTrafficFallback bool      `json:"naturalInvalidTrafficFallback"`
	ForcedSameSubjectDecoyRoute   bool      `json:"forcedSameSubjectDecoyRoute"`
	SameScopeRateLimit            bool      `json:"sameScopeRateLimit"`
	HardBlock                     bool      `json:"hardBlock"`
	Provenance                    string    `json:"provenance"`
	ObservedAt                    time.Time `json:"observedAt"`
	ExpiresAt                     time.Time `json:"expiresAt"`
	ReasonCodes                   []string  `json:"reasonCodes"`
}

func (value StrategyActionCapabilityV2) Validate(now time.Time) error {
	if value.Schema != StrategyActionCapabilitySchemaV2 || !ValidContractID(value.Strategy, 64) ||
		!ValidContractID(value.ActionRevision, 128) || !ValidContractID(value.StrategyRevision, 128) ||
		!ValidContractID(value.CapabilityRevision, 128) || !ValidContractID(value.Provenance, 128) ||
		!ValidContractID(value.ResourceID, 256) || !ValidContractID(value.EndpointID, 256) ||
		!ValidExactRevision(value.ActionScopeRevision) || !ValidExactRevision(value.EndpointRevision) ||
		!ValidExactRevision(value.ResourceRevision) || !ValidExactRevision(value.ConfigurationRevision) ||
		value.ObservedAt.IsZero() || value.ExpiresAt.IsZero() || !value.ExpiresAt.After(value.ObservedAt) ||
		value.ExpiresAt.After(value.ObservedAt.Add(24*time.Hour)) || value.ObservedAt.After(now.UTC().Add(5*time.Minute)) ||
		!validateReasonCodes(value.ReasonCodes) || !sortedUniqueStrings(value.ReasonCodes) {
		return errors.New("strategy action capability is invalid")
	}
	return nil
}

// PlannedResponseV2 is the resolver output. It deliberately cannot express
// APPLIED and is not an AppliedActionV1.
type PlannedResponseV2 struct {
	Schema                string          `json:"schema"`
	ResponseID            string          `json:"responseId"`
	DecisionID            string          `json:"decisionId"`
	ResourceID            string          `json:"resourceId"`
	EndpointID            string          `json:"endpointId"`
	Subject               SignalSubjectV2 `json:"subject"`
	DesiredIntent         ResponseIntent  `json:"desiredIntent"`
	SelectedIntent        ResponseIntent  `json:"selectedIntent"`
	CapabilityRevision    string          `json:"capabilityRevision"`
	PolicyRevision        string          `json:"policyRevision"`
	StrategyRevision      string          `json:"strategyRevision"`
	ActionScopeRevision   string          `json:"actionScopeRevision"`
	EndpointRevision      string          `json:"endpointRevision"`
	ResourceRevision      string          `json:"resourceRevision"`
	ConfigurationRevision string          `json:"configurationRevision"`
	ActualState           string          `json:"actualState"`
	ReasonCodes           []string        `json:"reasonCodes"`
	CreatedAt             time.Time       `json:"createdAt"`
	ExpiresAt             time.Time       `json:"expiresAt"`
}

func (value *PlannedResponseV2) FinalizeID() {
	payload, _ := json.Marshal(struct {
		DecisionID            string          `json:"decision_id"`
		ResourceID            string          `json:"resource_id"`
		EndpointID            string          `json:"endpoint_id"`
		Subject               SignalSubjectV2 `json:"subject"`
		DesiredIntent         ResponseIntent  `json:"desired_intent"`
		SelectedIntent        ResponseIntent  `json:"selected_intent"`
		CapabilityRevision    string          `json:"capability_revision"`
		PolicyRevision        string          `json:"policy_revision"`
		StrategyRevision      string          `json:"strategy_revision"`
		ActionScopeRevision   string          `json:"action_scope_revision"`
		EndpointRevision      string          `json:"endpoint_revision"`
		ResourceRevision      string          `json:"resource_revision"`
		ConfigurationRevision string          `json:"configuration_revision"`
		CreatedAt             time.Time       `json:"created_at"`
		ExpiresAt             time.Time       `json:"expires_at"`
	}{value.DecisionID, value.ResourceID, value.EndpointID, value.Subject, value.DesiredIntent, value.SelectedIntent, value.CapabilityRevision, value.PolicyRevision, value.StrategyRevision, value.ActionScopeRevision, value.EndpointRevision, value.ResourceRevision, value.ConfigurationRevision, value.CreatedAt.UTC(), value.ExpiresAt.UTC()})
	sum := sha256.Sum256(payload)
	value.ResponseID = hex.EncodeToString(sum[:])
}

func (value PlannedResponseV2) Validate() error {
	if value.Schema != PlannedResponseSchemaV2 || !ValidSHA256(value.ResponseID) || !ValidSHA256(value.DecisionID) ||
		!ValidContractID(value.ResourceID, 256) || !ValidContractID(value.EndpointID, 256) ||
		!ValidContractID(value.CapabilityRevision, 128) || !ValidContractID(value.PolicyRevision, 128) ||
		!ValidContractID(value.StrategyRevision, 128) || !ValidExactRevision(value.ActionScopeRevision) ||
		!ValidExactRevision(value.EndpointRevision) || !ValidExactRevision(value.ResourceRevision) ||
		!ValidExactRevision(value.ConfigurationRevision) || value.ActualState != "NOT_APPLIED" ||
		!validIntent(value.DesiredIntent) || !validIntent(value.SelectedIntent) ||
		!validateReasonCodes(value.ReasonCodes) || !sortedUniqueStrings(value.ReasonCodes) ||
		value.CreatedAt.IsZero() || value.ExpiresAt.IsZero() || !value.ExpiresAt.After(value.CreatedAt) ||
		value.ExpiresAt.After(value.CreatedAt.Add(24*time.Hour)) {
		return errors.New("planned response is invalid")
	}
	if err := validateSubject(value.Subject); err != nil {
		return err
	}
	expected := value
	expected.FinalizeID()
	if expected.ResponseID != value.ResponseID {
		return errors.New("planned response identity does not match immutable facts")
	}
	return nil
}

func CanonicalBoundedReasons(values ...string) []string {
	result := NormalizeActionReasons(values)
	if len(result) > maxReasonCodes {
		result = result[:maxReasonCodes]
	}
	return result
}

func CanonicalSignalRefs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, min(len(values), MaxGraylistSignalRefs))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !ValidSHA256(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) > MaxGraylistSignalRefs {
		result = result[:MaxGraylistSignalRefs]
	}
	return result
}

func CanonicalGraylistEvidenceRefs(values []GraylistEvidenceRefV2) []GraylistEvidenceRefV2 {
	bySignal := make(map[string]GraylistEvidenceRefV2, len(values))
	for _, value := range values {
		value.SignalID = strings.ToLower(strings.TrimSpace(value.SignalID))
		if !ValidSHA256(value.SignalID) || value.ExpiresAt.IsZero() {
			continue
		}
		if current, ok := bySignal[value.SignalID]; !ok || value.ExpiresAt.After(current.ExpiresAt) {
			bySignal[value.SignalID] = value
		}
	}
	result := make([]GraylistEvidenceRefV2, 0, min(len(bySignal), MaxGraylistSignalRefs))
	for _, value := range bySignal {
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].SignalID < result[right].SignalID })
	if len(result) > MaxGraylistSignalRefs {
		result = result[:MaxGraylistSignalRefs]
	}
	return result
}

func sortedUniqueSHA256(values []string) bool {
	for index, value := range values {
		if !ValidSHA256(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func sortedUniqueStrings(values []string) bool {
	for index, value := range values {
		if index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}
