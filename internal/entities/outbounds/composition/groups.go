// Package composition interprets native outbound groups as references to the
// existing Outbounds owner. Rendering borrows members through native selectors.
package composition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
)

func IsGroup(kind string) bool { return kind == "bond" || kind == "core-failover" }

type Member struct {
	Outbound      string `json:"outbound"`
	DownloadRatio int    `json:"download_ratio,omitempty"`
	UploadRatio   int    `json:"upload_ratio,omitempty"`
	Count         int    `json:"count,omitempty"`
}

func Members(kind string, raw json.RawMessage) ([]Member, error) {
	if !IsGroup(kind) {
		return nil, nil
	}
	if jsonvalue.StrictObject(raw) != nil {
		return nil, fmt.Errorf("OUTBOUND_GROUP_OPTIONS_INVALID")
	}
	var fields struct {
		Outbounds json.RawMessage `json:"outbounds"`
		Strategy  string          `json:"strategy"`
		Delay     string          `json:"delay"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return nil, fmt.Errorf("OUTBOUND_GROUP_OPTIONS_INVALID")
	}
	var result []Member
	if kind == "bond" {
		decoder := json.NewDecoder(bytes.NewReader(fields.Outbounds))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil {
			return nil, fmt.Errorf("OUTBOUND_GROUP_MEMBERS_INVALID")
		}
		legs, down, up := 0, 0, 0
		for _, member := range result {
			if member.Count < 0 || member.Count > 255 || member.DownloadRatio < 0 || member.DownloadRatio > 100 || member.UploadRatio < 0 || member.UploadRatio > 100 {
				return nil, fmt.Errorf("OUTBOUND_BOND_RATIOS_INVALID")
			}
			count := max(member.Count, 1)
			legs += count
			down += count * member.DownloadRatio
			up += count * member.UploadRatio
		}
		if legs < 1 || legs > 255 || down != 100 || up != 100 {
			return nil, fmt.Errorf("OUTBOUND_BOND_RATIOS_INVALID: weighted upload and download must each total 100")
		}
	} else {
		var tags []string
		if json.Unmarshal(fields.Outbounds, &tags) != nil {
			return nil, fmt.Errorf("OUTBOUND_GROUP_MEMBERS_INVALID")
		}
		for _, tag := range tags {
			result = append(result, Member{Outbound: tag})
		}
		if fields.Strategy != "" && fields.Strategy != "sequential" && fields.Strategy != "cycle" {
			return nil, fmt.Errorf("OUTBOUND_FAILOVER_STRATEGY_INVALID")
		}
		if fields.Delay != "" {
			delay, err := time.ParseDuration(fields.Delay)
			if err != nil || delay < 0 {
				return nil, fmt.Errorf("OUTBOUND_FAILOVER_DELAY_INVALID")
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("OUTBOUND_GROUP_MEMBERS_INVALID: select at least one canonical outbound")
	}
	for _, member := range result {
		if member.Outbound == "" || member.Outbound != strings.TrimSpace(member.Outbound) {
			return nil, fmt.Errorf("OUTBOUND_GROUP_MEMBERS_INVALID")
		}
	}
	return result, nil
}

type Index struct{ groups map[string][]Member }

func Build(rows []model.Outbound) (Index, error) {
	index := Index{groups: map[string][]Member{}}
	byTag := make(map[string]model.Outbound, len(rows))
	for _, row := range rows {
		if _, exists := byTag[row.Tag]; exists {
			return Index{}, fmt.Errorf("OUTBOUND_GROUP_AMBIGUOUS_TAG")
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
		for _, member := range members {
			if _, exists := byTag[member.Outbound]; !exists {
				return Index{}, fmt.Errorf("OUTBOUND_GROUP_MEMBER_MISSING: %q", member.Outbound)
			}
		}
		index.groups[row.Tag] = members
	}
	// Check native membership cycles. Tag-based dependencies remain interpreted
	// by tagrefs, including detours and the existing Panel failover policy.
	state := map[string]uint8{}
	var visit func(string, int) error
	visit = func(tag string, depth int) error {
		if state[tag] == 1 {
			return fmt.Errorf("OUTBOUND_GROUP_CYCLE: %q", tag)
		}
		if state[tag] == 2 {
			return nil
		}
		if depth > 64 {
			return fmt.Errorf("OUTBOUND_GROUP_DEPTH_EXCEEDED")
		}
		state[tag] = 1
		for _, member := range index.groups[tag] {
			if err := visit(member.Outbound, depth+1); err != nil {
				return err
			}
		}
		state[tag] = 2
		return nil
	}
	for tag := range index.groups {
		if err := visit(tag, 0); err != nil {
			return Index{}, err
		}
	}
	return index, nil
}

func (i Index) Members(tag string) []Member { return append([]Member(nil), i.groups[tag]...) }
