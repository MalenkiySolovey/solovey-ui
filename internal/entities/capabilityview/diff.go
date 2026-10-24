package capabilityview

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const (
	DiffSchema   = "solovey-ui/entity-build-capability-diff/v1"
	MaxDiffBytes = 16 << 10
)

// Difference is a projection of two owner observations, never a registration or
// permission. A loss does not authorize deleting a stored option. Missing facts
// remain UNKNOWN; target construction must still accept the preserved input.
type Difference struct {
	Schema       string   `json:"schema"`
	FromRevision string   `json:"fromRevision"`
	ToRevision   string   `json:"toRevision"`
	Changes      []Change `json:"changes"`
}

type Change struct {
	Category          string `json:"category"`
	Type              string `json:"type"`
	Before            State  `json:"before"`
	After             State  `json:"after"`
	BeforeReason      string `json:"beforeReason,omitempty"`
	AfterReason       string `json:"afterReason,omitempty"`
	BeforeRuntimeType string `json:"beforeRuntimeType"`
	AfterRuntimeType  string `json:"afterRuntimeType"`
	BeforeDependency  string `json:"beforeDependency,omitempty"`
	AfterDependency   string `json:"afterDependency,omitempty"`
}

func Compare(before, after Snapshot) (Difference, error) {
	if before.Validate(before.Provider, before.ComponentProfile) != nil || after.Validate(after.Provider, after.ComponentProfile) != nil ||
		before.ComponentProfile != after.ComponentProfile {
		return Difference{}, errors.New("capability comparison requires complete owner observations")
	}
	result := Difference{Schema: DiffSchema, FromRevision: before.Revision(), ToRevision: after.Revision(), Changes: []Change{}}
	// Both inputs are already sorted and unique. Merge their union without
	// inferring support from provider names or treating a missing fact as false.
	for left, right := 0, 0; left < len(before.Facts) || right < len(after.Facts); {
		var category, kind string
		switch {
		case right == len(after.Facts) || left < len(before.Facts) && compareFact(before.Facts[left], after.Facts[right]) < 0:
			category, kind = before.Facts[left].Category, before.Facts[left].Type
			left++
		case left == len(before.Facts) || compareFact(before.Facts[left], after.Facts[right]) > 0:
			category, kind = after.Facts[right].Category, after.Facts[right].Type
			right++
		default:
			category, kind = before.Facts[left].Category, before.Facts[left].Type
			left++
			right++
		}
		from, to := before.Resolve(category, kind), after.Resolve(category, kind)
		change := Change{Category: category, Type: kind, Before: from.State(), After: to.State(),
			BeforeReason: from.Reason, AfterReason: to.Reason, BeforeRuntimeType: from.RuntimeType, AfterRuntimeType: to.RuntimeType,
			BeforeDependency: from.RuntimeDependency, AfterDependency: to.RuntimeDependency}
		if change.Before != change.After || change.BeforeReason != change.AfterReason || change.BeforeRuntimeType != change.AfterRuntimeType ||
			change.BeforeDependency != change.AfterDependency {
			result.Changes = append(result.Changes, change)
		}
	}
	if err := result.Validate(result.FromRevision, result.ToRevision); err != nil {
		return Difference{}, err
	}
	return result, nil
}

func compareFact(a, b Fact) int {
	return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.Type, b.Type))
}

func (d Difference) Validate(from, to string) error {
	failed := errors.New("capability comparison is invalid")
	if d.Schema != DiffSchema || d.FromRevision != from || d.ToRevision != to || !revisionValid(from) || !revisionValid(to) ||
		d.Changes == nil || len(d.Changes) > MaxFacts*2 || from == to && len(d.Changes) != 0 {
		return failed
	}
	validState := func(s State) bool { return s == Supported || s == Unsupported || s == Unknown }
	optionalToken := func(s string) bool { return s == "" || token.MatchString(s) }
	for index, change := range d.Changes {
		if !token.MatchString(change.Category) || !token.MatchString(change.Type) || !validState(change.Before) || !validState(change.After) ||
			!optionalToken(change.BeforeReason) || !optionalToken(change.AfterReason) ||
			!token.MatchString(change.BeforeRuntimeType) || !token.MatchString(change.AfterRuntimeType) ||
			!optionalToken(change.BeforeDependency) || !optionalToken(change.AfterDependency) ||
			change.Before != Supported && change.BeforeReason == "" || change.After != Supported && change.AfterReason == "" ||
			change.Before == change.After && change.BeforeReason == change.AfterReason && change.BeforeRuntimeType == change.AfterRuntimeType &&
				change.BeforeDependency == change.AfterDependency {
			return failed
		}
		if index > 0 {
			previous := d.Changes[index-1]
			if cmp.Or(cmp.Compare(previous.Category, change.Category), cmp.Compare(previous.Type, change.Type)) >= 0 {
				return failed
			}
		}
	}
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > MaxDiffBytes {
		return failed
	}
	return nil
}

func revisionValid(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}
