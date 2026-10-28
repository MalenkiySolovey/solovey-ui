// Package runtimeprojection owns the stored entity eligibility projection.
// Entity adapters retain serialization; tagrefs retains reference interpretation.
package runtimeprojection

import (
	"errors"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/composition"
	outboundcomposition "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds/composition"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"gorm.io/gorm"
)

type Entity struct {
	Category   string                  `json:"category"`
	ID         uint                    `json:"id"`
	Type       string                  `json:"type"`
	Tag        string                  `json:"tag"`
	Capability entitycapabilities.Fact `json:"capability"`
}
type ReferenceIssue struct {
	Entity     Entity                 `json:"entity"`
	References []tagrefs.TagReference `json:"references"`
}
type Report struct {
	Omitted    []Entity         `json:"omitted"`
	References []ReferenceIssue `json:"references,omitempty"`
}
type Rejection struct{ Report Report }

func (e *Rejection) ReasonCode() string { return "CAPABILITY_REFERENCE_UNAVAILABLE" }
func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: %d unavailable entity reference(s); inspect runtime eligibility diagnostics", e.ReasonCode(), len(e.Report.References))
}

func Included(category, panelType string) bool {
	return entitycapabilities.Resolve(category, panelType).Available
}

// InboundEligibility keeps a group and its canonical members in one serving
// unit. An unavailable provider/capability cannot expose its children as
// independent listeners, or silently assemble a partial transport bundle.
func InboundEligibility(rows []model.Inbound) (map[string]entitycapabilities.Fact, error) {
	index, err := composition.Build(rows)
	if err != nil {
		return nil, err
	}
	facts := make(map[string]entitycapabilities.Fact, len(rows))
	for _, row := range rows {
		facts[row.Tag] = entitycapabilities.ResolveResource("inbounds", row.Type, row.Options)
	}
	for _, row := range rows {
		if !composition.IsGroup(row.Type) || !facts[row.Tag].Available {
			continue
		}
		for _, member := range index.Members(row.Tag) {
			if !facts[member].Available {
				fact := facts[row.Tag]
				fact.Available, fact.Reason = false, "INBOUND_GROUP_MEMBER_UNAVAILABLE"
				facts[row.Tag] = fact
				break
			}
		}
	}
	for _, row := range rows {
		if parent := index.Parent(row.Tag); parent != "" && !facts[parent].Available {
			fact := facts[row.Tag]
			fact.Available, fact.Reason = false, "INBOUND_GROUP_UNAVAILABLE"
			facts[row.Tag] = fact
		}
	}
	return facts, nil
}

func Stored(db *gorm.DB) ([]Entity, error) {
	if db == nil {
		return nil, errors.New("entity persistence is unavailable")
	}
	result := []Entity{}
	for _, entry := range []struct {
		category string
		model    any
	}{
		{"inbounds", &model.Inbound{}}, {"outbounds", &model.Outbound{}}, {"endpoints", &model.Endpoint{}}, {"services", &model.Service{}},
	} {
		if !db.Migrator().HasTable(entry.model) {
			continue
		}
		var rows []struct {
			ID        uint
			Type, Tag string
		}
		if err := db.Model(entry.model).Select("id", "type", "tag").Order("id").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result = append(result, Entity{Category: entry.category, ID: row.ID, Type: row.Type, Tag: row.Tag, Capability: entitycapabilities.Resolve(entry.category, row.Type)})
		}
	}
	if db.Migrator().HasTable(&model.Inbound{}) {
		var inbounds []model.Inbound
		if err := db.Select("id", "type", "tag", "options").Find(&inbounds).Error; err != nil {
			return nil, err
		}
		facts, err := InboundEligibility(inbounds)
		if err != nil {
			return nil, err
		}
		for i := range result {
			if result[i].Category == "inbounds" {
				result[i].Capability = facts[result[i].Tag]
			}
		}
	}
	if db.Migrator().HasTable(&model.Outbound{}) {
		var outbounds []model.Outbound
		if err := db.Select("id", "type", "tag", "options").Find(&outbounds).Error; err != nil {
			return nil, err
		}
		facts, err := OutboundEligibility(outbounds)
		if err != nil {
			return nil, err
		}
		for i := range result {
			if result[i].Category == "outbounds" {
				result[i].Capability = facts[result[i].Tag]
			}
		}
	}
	return result, nil
}

// Outbound members remain independently usable. Only the native group which
// embeds an unavailable member is omitted, including transitive native groups.
func OutboundEligibility(rows []model.Outbound) (map[string]entitycapabilities.Fact, error) {
	index, err := outboundcomposition.Build(rows)
	if err != nil {
		return nil, err
	}
	if err := tagrefs.ValidateNativeOutboundGroups(rows); err != nil {
		return nil, err
	}
	facts := make(map[string]entitycapabilities.Fact, len(rows))
	for _, row := range rows {
		facts[row.Tag] = entitycapabilities.ResolveResource("outbounds", row.Type, row.Options)
	}
	done := map[string]bool{}
	var resolve func(string) entitycapabilities.Fact
	resolve = func(tag string) entitycapabilities.Fact {
		fact := facts[tag]
		if done[tag] {
			return fact
		}
		done[tag] = true
		for _, member := range index.Members(tag) {
			if !resolve(member.Outbound).Available && fact.Available {
				fact.Available, fact.Reason = false, "OUTBOUND_GROUP_MEMBER_UNAVAILABLE"
			}
		}
		facts[tag] = fact
		return fact
	}
	for tag := range facts {
		resolve(tag)
	}
	return facts, nil
}

func Omissions(rows []Entity) []Entity {
	result := []Entity{}
	for _, row := range rows {
		if !row.Capability.Available {
			result = append(result, row)
		}
	}
	return result
}

// ValidateReferences checks the candidate database view and supplied base
// config without preparing rule sets, committing rows or mutating the core.
// A nil base means the stored base; a supplied candidate is authoritative.
func ValidateReferences(db *gorm.DB, base []byte) (Report, error) {
	rows, err := Stored(db)
	if err != nil {
		return Report{}, err
	}
	report := Report{Omitted: Omissions(rows)}
	if len(report.Omitted) == 0 {
		return report, nil
	}
	if base == nil && db.Migrator().HasTable(&model.Setting{}) {
		var setting model.Setting
		if err := db.Model(&model.Setting{}).Where("key = ?", "config").Limit(1).Find(&setting).Error; err != nil {
			return report, err
		}
		base = []byte(setting.Value)
	}
	ids := map[string][]uint{}
	for _, row := range rows {
		if row.Capability.Available {
			ids[row.Category] = append(ids[row.Category], row.ID)
		}
	}
	var projected tagrefs.ProjectionRows
	for _, entry := range []struct {
		category    string
		destination any
	}{
		{"inbounds", &projected.Inbounds}, {"outbounds", &projected.Outbounds}, {"endpoints", &projected.Endpoints}, {"services", &projected.Services},
	} {
		if len(ids[entry.category]) == 0 {
			continue
		}
		if err := db.Where("id IN ?", ids[entry.category]).Find(entry.destination).Error; err != nil {
			return report, err
		}
	}
	for _, omitted := range report.Omitted {
		refs, err := tagrefs.ProjectedReferences(base, projected, omitted.Category, omitted.Tag)
		if err != nil {
			return report, err
		}
		// A Users assignment is owned desired state, not a runtime reference:
		// credentials are projected only with their included serving inbound.
		// Preserve that assignment while its inbound is inactive. Actual route,
		// DNS and runtime entity references below still fail closed.
		if len(refs) > 0 {
			report.References = append(report.References, ReferenceIssue{Entity: omitted, References: refs})
		}
	}
	if len(report.References) > 0 {
		return report, &Rejection{Report: report}
	}
	return report, nil
}
