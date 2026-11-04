package outbounds

import (
	"encoding/json"
	"fmt"
	"strings"

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
	if composition.IsLimiter(candidate.Type) {
		var fields struct {
			Strategy string
			Route    struct{ Final string }
			Users    []struct{ Name, Strategy string }
		}
		if json.Unmarshal(candidate.Options, &fields) != nil || fields.Route.Final == "" {
			return fmt.Errorf("OUTBOUND_LIMITER_ROUTE_INVALID")
		}
		// The foreign manager strategy belongs to its separate management plane;
		// Solovey Users/quotas remain the only live account authority.
		if fields.Strategy == "manager" {
			return fmt.Errorf("OUTBOUND_LIMITER_MANAGER_UNAVAILABLE")
		}
		if fields.Strategy == "users" {
			if len(fields.Users) == 0 {
				return fmt.Errorf("OUTBOUND_LIMITER_USERS_INVALID")
			}
			names := map[string]bool{}
			for _, user := range fields.Users {
				if strings.TrimSpace(user.Name) == "" || names[user.Name] || user.Strategy == "manager" || user.Strategy == "users" {
					return fmt.Errorf("OUTBOUND_LIMITER_USERS_INVALID")
				}
				names[user.Name] = true
			}
		}
		refs, err := composition.LimiterReferences(candidate.Options)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, row := range rows {
			known[row.Tag] = true
		}
		var endpoints []model.Endpoint
		if err := db.Select("tag").Find(&endpoints).Error; err != nil {
			return err
		}
		for _, row := range endpoints {
			known[row.Tag] = true
		}
		for _, tag := range refs {
			if !known[tag] {
				return fmt.Errorf("OUTBOUND_LIMITER_MEMBER_MISSING")
			}
		}
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
	return v.groups.Render(row.Type, row.Tag, raw)
}
