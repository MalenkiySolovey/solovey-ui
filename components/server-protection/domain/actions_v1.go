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

const AppliedActionSchemaV1 = "solovey-ui/applied-action/v1"

type AppliedActionState string

const (
	ActionPlanned    AppliedActionState = "PLANNED"
	ActionVerified   AppliedActionState = "VERIFIED"
	ActionApplied    AppliedActionState = "APPLIED"
	ActionExpired    AppliedActionState = "EXPIRED"
	ActionRolledBack AppliedActionState = "ROLLED_BACK"
	ActionFailed     AppliedActionState = "FAILED"
)

// AppliedActionV1 is deliberately separate from ProtectionDecisionV2. A
// capability resolution may produce a PLANNED action, but only executor
// verification with an exact actual revision may produce APPLIED.
type AppliedActionV1 struct {
	Schema     string          `json:"schema"`
	ActionID   string          `json:"actionId"`
	DecisionID string          `json:"decisionId"`
	PlanDigest string          `json:"planDigest"`
	ResourceID string          `json:"resourceId"`
	Subject    SignalSubjectV2 `json:"subject"`
	// GraphRevision is the legacy JSON name for the exact targeting-scope
	// revision. Additive firewall actions bind configured endpoint scope;
	// topology-changing actions may bind an exact ownership graph.
	GraphRevision         string             `json:"graphRevision"`
	EndpointRevision      string             `json:"endpointRevision"`
	ResourceRevision      string             `json:"resourceRevision"`
	ConfigurationRevision string             `json:"configurationRevision"`
	RequestedIntent       ResponseIntent     `json:"requestedIntent"`
	ResolvedIntent        ResponseIntent     `json:"resolvedIntent"`
	DesiredState          string             `json:"desiredState"`
	SelectedState         string             `json:"selectedState"`
	ActualState           string             `json:"actualState"`
	ActualRevision        string             `json:"actualRevision,omitempty"`
	State                 AppliedActionState `json:"state"`
	CreatedAt             time.Time          `json:"createdAt"`
	ExpiresAt             time.Time          `json:"expiresAt"`
	VerifiedAt            *time.Time         `json:"verifiedAt,omitempty"`
	ReasonCodes           []string           `json:"reasonCodes,omitempty"`
}

func (action *AppliedActionV1) FinalizeID() {
	payload, _ := json.Marshal(struct {
		DecisionID            string          `json:"decision_id"`
		PlanDigest            string          `json:"plan_digest"`
		ResourceID            string          `json:"resource_id"`
		Subject               SignalSubjectV2 `json:"subject"`
		GraphRevision         string          `json:"graph_revision"`
		EndpointRevision      string          `json:"endpoint_revision"`
		ResourceRevision      string          `json:"resource_revision"`
		ConfigurationRevision string          `json:"configuration_revision"`
		RequestedIntent       ResponseIntent  `json:"requested_intent"`
		ResolvedIntent        ResponseIntent  `json:"resolved_intent"`
		CreatedAt             time.Time       `json:"created_at"`
		ExpiresAt             time.Time       `json:"expires_at"`
	}{action.DecisionID, action.PlanDigest, action.ResourceID, action.Subject, action.GraphRevision, action.EndpointRevision, action.ResourceRevision, action.ConfigurationRevision, action.RequestedIntent, action.ResolvedIntent, action.CreatedAt.UTC(), action.ExpiresAt.UTC()})
	sum := sha256.Sum256(payload)
	action.ActionID = hex.EncodeToString(sum[:])
}

func (action AppliedActionV1) Validate(now time.Time) error {
	if action.Schema != AppliedActionSchemaV1 || !ValidSHA256(action.ActionID) || !ValidSHA256(action.DecisionID) || !ValidSHA256(action.PlanDigest) {
		return errors.New("action schema or identity is invalid")
	}
	expected := action
	expected.FinalizeID()
	if expected.ActionID != action.ActionID {
		return errors.New("action identity does not match its immutable contract")
	}
	if !ValidContractID(action.ResourceID, maxContractIDBytes) || !validRevision(action.GraphRevision) || !validRevision(action.EndpointRevision) || !validRevision(action.ResourceRevision) || !validRevision(action.ConfigurationRevision) {
		return errors.New("action target revisions are invalid")
	}
	if err := validateSubject(action.Subject); err != nil || action.Subject.Type != "ip" && action.Subject.Type != "prefix" {
		return errors.New("action subject is invalid")
	}
	if !validIntent(action.RequestedIntent) || !validIntent(action.ResolvedIntent) || action.ResolvedIntent == IntentObserve {
		return errors.New("action intent is invalid")
	}
	if action.CreatedAt.IsZero() || action.ExpiresAt.IsZero() || !action.ExpiresAt.After(action.CreatedAt) || action.ExpiresAt.After(action.CreatedAt.Add(24*time.Hour)) || action.CreatedAt.After(now.UTC().Add(5*time.Minute)) {
		return errors.New("action timestamps are invalid")
	}
	if !validActionStatus(action.DesiredState) || !validActionStatus(action.SelectedState) || !validActionStatus(action.ActualState) || !validateReasonCodes(action.ReasonCodes) {
		return errors.New("action status is invalid")
	}
	switch action.State {
	case ActionPlanned:
		if action.ActualState != "NOT_APPLIED" || action.ActualRevision != "" || action.VerifiedAt != nil {
			return errors.New("planned action cannot claim actual state")
		}
	case ActionVerified:
		if action.VerifiedAt == nil || action.ActualRevision == "" || action.ActualState != "VERIFIED_NOT_APPLIED" || !validRevision(action.ActualRevision) {
			return errors.New("verified action evidence is incomplete")
		}
	case ActionApplied:
		if action.VerifiedAt == nil || action.ActualRevision == "" || action.ActualState != "APPLIED" || !validRevision(action.ActualRevision) {
			return errors.New("applied action lacks exact verification")
		}
	case ActionExpired, ActionRolledBack, ActionFailed:
		if action.ActualState == "APPLIED" {
			return errors.New("terminal action cannot claim applied state")
		}
	default:
		return errors.New("action state is invalid")
	}
	return nil
}

func validRevision(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'f') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validActionStatus(value string) bool {
	switch value {
	case "REQUESTED", "SELECTED", "NOT_APPLIED", "VERIFIED_NOT_APPLIED", "APPLIED", "EXPIRED", "ROLLED_BACK", "FAILED":
		return true
	default:
		return false
	}
}

func NormalizeActionReasons(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || !ValidContractID(value, 128) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 32 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func (action AppliedActionV1) MarkVerified(actualRevision string, at time.Time) (AppliedActionV1, error) {
	if action.Validate(at) != nil || action.State != ActionPlanned || !validRevision(actualRevision) || at.IsZero() || at.Before(action.CreatedAt) || !at.Before(action.ExpiresAt) {
		return action, errors.New("planned action cannot be verified with the supplied exact revision")
	}
	verified := at.UTC()
	action.State = ActionVerified
	action.ActualState = "VERIFIED_NOT_APPLIED"
	action.ActualRevision = actualRevision
	action.VerifiedAt = &verified
	return action, nil
}

func (action AppliedActionV1) MarkApplied(actualRevision string, at time.Time) (AppliedActionV1, error) {
	if action.Validate(at) != nil || action.State != ActionVerified || action.VerifiedAt == nil || action.ActualRevision != actualRevision || !validRevision(actualRevision) || at.IsZero() || at.Before(*action.VerifiedAt) || !at.Before(action.ExpiresAt) {
		return action, errors.New("action cannot become APPLIED without matching verified actual state")
	}
	action.State = ActionApplied
	action.ActualState = "APPLIED"
	return action, nil
}

func (action AppliedActionV1) MarkTerminal(state AppliedActionState, reason string) (AppliedActionV1, error) {
	if err := action.Validate(action.ExpiresAt); err != nil {
		return action, errors.New("action identity is invalid")
	}
	switch state {
	case ActionExpired:
		action.ActualState = "EXPIRED"
	case ActionRolledBack:
		action.ActualState = "ROLLED_BACK"
	case ActionFailed:
		action.ActualState = "FAILED"
	default:
		return action, errors.New("requested action terminal state is invalid")
	}
	action.State = state
	action.ReasonCodes = NormalizeActionReasons(append(action.ReasonCodes, reason))
	return action, nil
}
