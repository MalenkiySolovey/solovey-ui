// Package enforcement compiles feature policy into bounded host-network requests.
// It cannot apply a request, observe the OS, access storage or authorize a broker.
package enforcement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type DynamicSetLimits struct {
	MaxElements       int `json:"maxElements"`
	DefaultTTLSeconds int `json:"defaultTtlSeconds"`
	MaxTTLSeconds     int `json:"maxTtlSeconds"`
}

type EndpointContribution struct {
	ContributionID string                `json:"contributionId"`
	ActionID       string                `json:"actionId,omitempty"`
	ActionIDs      []string              `json:"actionIds,omitempty"`
	DecisionID     string                `json:"decisionId,omitempty"`
	DecisionIDs    []string              `json:"decisionIds,omitempty"`
	RefCount       int                   `json:"refCount"`
	Subject        string                `json:"subject"`
	Intent         domain.ResponseIntent `json:"intent"`
	ExpiresAt      int64                 `json:"expiresAt"`
	TTLSeconds     int                   `json:"ttlSeconds"`
	SourceClass    string                `json:"sourceClass"`
	SourceClasses  []string              `json:"sourceClasses,omitempty"`
}

type EndpointActionInput struct {
	Action      domain.AppliedActionV1
	SourceClass string
}

type AdmittedAction struct {
	Action       domain.AppliedActionV1
	SourceClass  string
	Contribution EndpointContribution
}

// Target binds a contribution to the current canonical resource and exact
// endpoint. The caller obtains these facts from the existing owner inventory.
type Target struct {
	ResourceID, ActionScopeRevision, EndpointRevision, ResourceRevision, ConfigurationRevision string
}

// AdmitActions validates the existing action contract, scope, lifetime and source.
// These are desired contributions, never evidence of successful application.
func AdmitActions(values []EndpointActionInput, actionScopeRevision string, now time.Time, limits DynamicSetLimits) (map[string][]AdmittedAction, []string) {
	result := make(map[string][]AdmittedAction)
	reasons := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range values {
		action := value.Action
		if !action.ExpiresAt.After(now) {
			continue
		}
		if err := action.Validate(now); err != nil || action.State != domain.ActionPlanned && action.State != domain.ActionVerified || action.GraphRevision != actionScopeRevision || action.ExpiresAt.After(now.Add(time.Duration(limits.MaxTTLSeconds)*time.Second)) || value.SourceClass != "native" && value.SourceClass != "external" {
			reasons = append(reasons, "action_contract_invalid")
			continue
		}
		subject := canonicalActionSubject(action.Subject)
		if subject == "" || !Materializes(action.ResolvedIntent) {
			reasons = append(reasons, "action_contract_invalid")
			continue
		}
		if _, exists := seen[action.ActionID]; exists {
			continue
		}
		seen[action.ActionID] = struct{}{}
		ttl := int(action.ExpiresAt.Sub(now) / time.Second)
		if ttl < 1 {
			ttl = 1
		}
		if ttl > limits.MaxTTLSeconds {
			ttl = limits.MaxTTLSeconds
		}
		contribution := EndpointContribution{ContributionID: action.ActionID, ActionID: action.ActionID, ActionIDs: []string{action.ActionID}, DecisionID: action.DecisionID, DecisionIDs: []string{action.DecisionID}, RefCount: 1, Subject: subject, Intent: action.ResolvedIntent, ExpiresAt: action.ExpiresAt.UTC().Unix(), TTLSeconds: ttl, SourceClass: value.SourceClass, SourceClasses: []string{value.SourceClass}}
		result[action.EndpointRevision] = append(result[action.EndpointRevision], AdmittedAction{Action: action, SourceClass: value.SourceClass, Contribution: contribution})
	}
	for endpointRevision := range result {
		sort.Slice(result[endpointRevision], func(i, j int) bool {
			return result[endpointRevision][i].Action.ActionID < result[endpointRevision][j].Action.ActionID
		})
	}
	return result, uniqueSorted(reasons)
}

// BindActions preserves the resource/revision fence before coalescing equal
// endpoint contributions. An admitted contract is not a current target grant.
func BindActions(target Target, actions []AdmittedAction) ([]EndpointContribution, []string) {
	values := make([]EndpointContribution, 0)
	reasons := make([]string, 0)
	for _, action := range actions {
		if action.Action.ResourceID != target.ResourceID || action.Action.GraphRevision != target.ActionScopeRevision ||
			action.Action.EndpointRevision != target.EndpointRevision || action.Action.ResourceRevision != target.ResourceRevision ||
			action.Action.ConfigurationRevision != target.ConfigurationRevision {
			reasons = append(reasons, "action_revision_stale")
			continue
		}
		values = append(values, action.Contribution)
	}
	return collapseContributions(target.ResourceID+"\x00"+target.EndpointRevision, values), uniqueSorted(reasons)
}

func collapseContributions(resourceID string, values []EndpointContribution) []EndpointContribution {
	groups := make(map[string]EndpointContribution)
	actions := make(map[string]map[string]struct{})
	decisions := make(map[string]map[string]struct{})
	sources := make(map[string]map[string]struct{})
	for _, value := range values {
		key := value.Subject + "\x00" + string(value.Intent)
		group := groups[key]
		if group.Subject == "" {
			payload, _ := json.Marshal(struct{ ResourceID, Subject, Intent string }{resourceID, value.Subject, string(value.Intent)})
			sum := sha256.Sum256(payload)
			group = value
			group.ContributionID = hex.EncodeToString(sum[:])
			group.ActionID = ""
			group.ActionIDs = nil
			group.DecisionID = ""
			group.DecisionIDs = nil
			group.RefCount = 0
			actions[key] = make(map[string]struct{})
			decisions[key] = make(map[string]struct{})
			sources[key] = make(map[string]struct{})
		}
		if value.DecisionID != "" {
			decisions[key][value.DecisionID] = struct{}{}
		}
		for _, actionID := range append([]string{value.ActionID}, value.ActionIDs...) {
			if actionID != "" {
				actions[key][actionID] = struct{}{}
			}
		}
		if value.SourceClass != "" {
			sources[key][value.SourceClass] = struct{}{}
		}
		if value.ExpiresAt > group.ExpiresAt {
			group.ExpiresAt = value.ExpiresAt
		}
		if value.TTLSeconds > group.TTLSeconds {
			group.TTLSeconds = value.TTLSeconds
		}
		groups[key] = group
	}
	result := make([]EndpointContribution, 0, len(groups))
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		for actionID := range actions[key] {
			group.ActionIDs = append(group.ActionIDs, actionID)
		}
		for decisionID := range decisions[key] {
			group.DecisionIDs = append(group.DecisionIDs, decisionID)
		}
		for sourceClass := range sources[key] {
			group.SourceClasses = append(group.SourceClasses, sourceClass)
		}
		sort.Strings(group.SourceClasses)
		group.SourceClass = ""
		if len(group.SourceClasses) > 0 {
			group.SourceClass = group.SourceClasses[0]
		}
		sort.Strings(group.DecisionIDs)
		sort.Strings(group.ActionIDs)
		group.RefCount = len(group.ActionIDs)
		if group.RefCount == 0 {
			group.RefCount = 1
		}
		result = append(result, group)
	}
	return result
}

func Materializes(value domain.ResponseIntent) bool {
	return value == domain.IntentSoftGraylist || value == domain.IntentRateLimit || value == domain.IntentTemporaryQuarantine || value == domain.IntentTemporaryBlock
}

func canonicalActionSubject(value domain.SignalSubjectV2) string {
	switch value.Type {
	case "ip":
		address, err := netip.ParseAddr(value.Value)
		if err != nil || address.Unmap().String() != value.Value {
			return ""
		}
		bits := 128
		if address.Is4() {
			bits = 32
		}
		return netip.PrefixFrom(address, bits).String()
	case "prefix":
		prefix, err := netip.ParsePrefix(value.Value)
		if err != nil || prefix.Masked().String() != value.Value {
			return ""
		}
		return prefix.String()
	default:
		return ""
	}
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
