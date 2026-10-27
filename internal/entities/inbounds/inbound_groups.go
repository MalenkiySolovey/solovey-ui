package entityinbounds

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/composition"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"gorm.io/gorm"
)

var ErrInboundEndpointLeased = errors.New("inbound_endpoint_lease_active")

// Structural composition affects every old and new member's serving owner.
// Keep this admission at the entity boundary so imports use the same guard.
func validateInboundGroupLeases(db *gorm.DB, previous, next model.Inbound) error {
	if !composition.IsGroup(previous.Type) && !composition.IsGroup(next.Type) {
		return nil
	}
	tags, err := composition.Members(previous.Type, previous.Options)
	if err != nil {
		return err
	}
	members, err := composition.Members(next.Type, next.Options)
	if err != nil {
		return err
	}
	tags = append(tags, members...)
	if !db.Migrator().HasTable(&model.InboundEndpointLease{}) {
		return nil
	}
	var count int64
	if err := db.Model(&model.InboundEndpointLease{}).
		Where("inbound_id IN (?)", db.Model(&model.Inbound{}).Select("id").Where("tag IN ?", tags)).
		Where("state <> ?", "RELEASED").Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrInboundEndpointLeased
	}
	return nil
}

func validateInboundGroupCandidate(db *gorm.DB, candidate model.Inbound) error {
	var rows []model.Inbound
	if err := db.Select("id", "type", "tag", "options").Find(&rows).Error; err != nil {
		return err
	}
	replaced := false
	var previous model.Inbound
	for i := range rows {
		if candidate.Id != 0 && rows[i].Id == candidate.Id {
			previous = rows[i]
			rows[i], replaced = candidate, true
		}
	}
	if !replaced {
		rows = append(rows, candidate)
	}
	index, err := composition.Build(rows)
	if err != nil {
		return err
	}
	if err := validateInboundGroupLeases(db, previous, candidate); err != nil {
		return err
	}
	for _, row := range rows {
		if index.Parent(row.Tag) == "" || !db.Migrator().HasTable(&model.Service{}) {
			continue
		}
		services, err := tagrefs.SSMCascadeServiceIDs(db, row.Tag)
		if err != nil {
			return err
		}
		if len(services) != 0 {
			return fmt.Errorf("INBOUND_GROUP_MEMBER_EXTERNALLY_MANAGED: %q is managed by SSM API", row.Tag)
		}
	}
	return nil
}

type runtimeInboundView struct {
	rows   []model.Inbound
	byTag  map[string]model.Inbound
	groups composition.Index
	facts  map[string]entitycapabilities.Fact
}

func loadRuntimeInboundView(db *gorm.DB) (runtimeInboundView, error) {
	view := runtimeInboundView{byTag: map[string]model.Inbound{}}
	if err := db.Preload("Tls").Order(entityorder.Clause).Find(&view.rows).Error; err != nil {
		return view, err
	}
	var err error
	view.groups, err = composition.Build(view.rows)
	if err != nil {
		return view, err
	}
	view.facts, err = runtimeprojection.InboundEligibility(view.rows)
	for _, row := range view.rows {
		view.byTag[row.Tag] = row
	}
	return view, err
}

// Members retain their own TLS, credential and Users owners. Only this final
// runtime projection embeds their resolved options in the native group.
func (v runtimeInboundView) render(db *gorm.DB, row model.Inbound, hooks UserHooks) (json.RawMessage, error) {
	if !v.facts[row.Tag].Available {
		return nil, fmt.Errorf("INBOUND_GROUP_MEMBER_UNAVAILABLE")
	}
	raw, err := marshalRuntimeInbound(db, row)
	if err != nil {
		return nil, err
	}
	if !composition.IsGroup(row.Type) {
		if hooks != nil {
			return hooks.AddUsers(db, raw, row.Id, row.Type)
		}
		return raw, nil
	}
	members := v.groups.Members(row.Tag)
	children := make([]json.RawMessage, 0, len(members))
	for _, tag := range members {
		child, err := v.render(db, v.byTag[tag], hooks)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	config["inbounds"], err = json.Marshal(children)
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}
