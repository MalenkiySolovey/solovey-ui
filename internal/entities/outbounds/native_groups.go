package outbounds

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds/composition"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"gorm.io/gorm"
)

func validateNativeGroupCandidate(db *gorm.DB, candidate model.Outbound) error {
	var rows []model.Outbound
	if err := db.Select("id", "type", "tag", "options").Find(&rows).Error; err != nil {
		return err
	}
	replaced := false
	for i := range rows {
		if candidate.Id != 0 && rows[i].Id == candidate.Id {
			rows[i], replaced = candidate, true
		}
	}
	if !replaced {
		rows = append(rows, candidate)
	}
	if _, err := composition.Build(rows); err != nil {
		return err
	}
	return tagrefs.ValidateNativeOutboundGroups(rows)
}

type runtimeOutboundView struct {
	groups    composition.Index
	facts     map[string]entitycapabilities.Fact
	directTag string
}

func loadRuntimeOutboundView(db *gorm.DB) (runtimeOutboundView, error) {
	view := runtimeOutboundView{directTag: DirectFallbackTag(db)}
	var rows []model.Outbound
	if err := db.Order(entityorder.Clause).Find(&rows).Error; err != nil {
		return view, err
	}
	var err error
	view.groups, err = composition.Build(rows)
	if err != nil {
		return view, err
	}
	view.facts, err = runtimeprojection.OutboundEligibility(rows)
	return view, err
}

// Canonical members retain one runtime instance and credential owner. Anonymous
// native selectors borrow those instances, including their mutable selection
// and Panel health policy, while the outer group owns the native wire protocol.
func (v runtimeOutboundView) render(db *gorm.DB, row model.Outbound) (json.RawMessage, error) {
	if !v.facts[row.Tag].Available {
		return nil, fmt.Errorf("OUTBOUND_GROUP_MEMBER_UNAVAILABLE")
	}
	if row.Type == FailoverType {
		return AssembleFailoverForCore(row, v.directTag)
	}
	raw, err := row.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if len(managedCredentialFields(row.Type)) != 0 {
		raw, err = marshalManagedRuntime(db, row, raw)
		if err != nil {
			return nil, err
		}
	}
	raw, err = marshalTransportRuntime(db, row, raw)
	if err != nil {
		return nil, err
	}
	if !composition.IsGroup(row.Type) {
		return raw, nil
	}
	var children []json.RawMessage
	for _, member := range v.groups.Members(row.Tag) {
		child, err := json.Marshal(struct {
			Type      string   `json:"type"`
			Outbounds []string `json:"outbounds"`
			Default   string   `json:"default"`
		}{"selector", []string{member.Outbound}, member.Outbound})
		if err != nil {
			return nil, err
		}
		if row.Type == "bond" {
			child, err = json.Marshal(struct {
				Outbound      json.RawMessage `json:"outbound"`
				DownloadRatio int             `json:"download_ratio"`
				UploadRatio   int             `json:"upload_ratio"`
				Count         int             `json:"count,omitempty"`
			}{child, member.DownloadRatio, member.UploadRatio, member.Count})
			if err != nil {
				return nil, err
			}
		}
		children = append(children, child)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if row.Type == "core-failover" {
		config["type"] = json.RawMessage(`"failover"`)
	}
	config["outbounds"], err = json.Marshal(children)
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}
