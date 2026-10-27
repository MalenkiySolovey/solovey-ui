// Package composition interprets canonical inbound group member references.
// It never embeds copied child state in persistence or creates runtime owners.
package composition

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
)

// Core failover is the engine's connection wrapper, distinct from the existing
// Panel outbound Failover policy and its unresolved omitted-final semantics.
func IsGroup(kind string) bool { return kind == "bond" || kind == "core-failover" }

func Members(kind string, options json.RawMessage) ([]string, error) {
	if !IsGroup(kind) {
		return nil, nil
	}
	if jsonvalue.StrictObject(options) != nil {
		return nil, fmt.Errorf("INBOUND_GROUP_OPTIONS_INVALID")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(options, &fields)
	var members []string
	if json.Unmarshal(fields["inbounds"], &members) != nil || len(members) == 0 || len(members) > 255 {
		return nil, fmt.Errorf("INBOUND_GROUP_MEMBERS_INVALID: select 1 to 255 canonical inbound tags")
	}
	seen := map[string]bool{}
	for _, tag := range members {
		if tag == "" || tag != strings.TrimSpace(tag) || seen[tag] {
			return nil, fmt.Errorf("INBOUND_GROUP_MEMBERS_INVALID: empty or duplicate member")
		}
		seen[tag] = true
	}
	return members, nil
}

type Index struct {
	members map[string][]string
	parents map[string]string
}

// Build checks the complete stored view, including unavailable historical
// groups. Provider projection cannot accidentally expose their children as
// independent listeners when the owning group is unavailable.
func Build(rows []model.Inbound) (Index, error) {
	index := Index{members: map[string][]string{}, parents: map[string]string{}}
	byTag := make(map[string]model.Inbound, len(rows))
	for _, row := range rows {
		if _, duplicate := byTag[row.Tag]; duplicate {
			return Index{}, fmt.Errorf("INBOUND_GROUP_AMBIGUOUS_TAG: %q", row.Tag)
		}
		byTag[row.Tag] = row
	}
	for _, row := range rows {
		if !IsGroup(row.Type) {
			continue
		}
		members, err := Members(row.Type, row.Options)
		if err != nil {
			return Index{}, err
		}
		for _, tag := range members {
			member, found := byTag[tag]
			if !found {
				return Index{}, fmt.Errorf("INBOUND_GROUP_MEMBER_MISSING: %q", tag)
			}
			if tag == row.Tag || IsGroup(member.Type) {
				return Index{}, fmt.Errorf("INBOUND_GROUP_NESTING_INVALID: %q", tag)
			}
			if parent := index.parents[tag]; parent != "" {
				return Index{}, fmt.Errorf("INBOUND_GROUP_MEMBER_ALREADY_OWNED: %q", tag)
			}
			index.parents[tag] = row.Tag
		}
		index.members[row.Tag] = members
	}
	return index, nil
}

func (i Index) Parent(tag string) string    { return i.parents[tag] }
func (i Index) Members(tag string) []string { return slices.Clone(i.members[tag]) }

// Root preserves an ordinary inbound tag and resolves a member to its one
// owning group. Consumers use the same result for rendering and reapply.
func (i Index) Root(tag string) string {
	if parent := i.Parent(tag); parent != "" {
		return parent
	}
	return tag
}
