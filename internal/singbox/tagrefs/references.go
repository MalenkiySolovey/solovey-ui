package tagrefs

import (
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

// TagReference describes one stored configuration site that points at a tag.
// Lazy references are resolved by sing-box at use time. Eager references are
// captured when an adapter is built, so replacing that adapter needs a restart.
type TagReference struct {
	Kind    string
	Locator string
	Lazy    bool
}

func Eager(refs []TagReference) []TagReference {
	eager := make([]TagReference, 0, len(refs))
	for _, ref := range refs {
		if !ref.Lazy {
			eager = append(eager, ref)
		}
	}
	return eager
}

func FormatError(entityKind string, tag string, refs []TagReference) error {
	locators := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if _, ok := seen[ref.Locator]; ok {
			continue
		}
		seen[ref.Locator] = struct{}{}
		locators = append(locators, ref.Locator)
	}
	sort.Strings(locators)

	hint := "remove the reference or point it to another " + entityKind + " first"
	if entityKind == "outbound" || entityKind == "endpoint" {
		hint = "remove the reference or point it to another outbound (for example direct) first"
	}
	return common.NewErrorf("%s %q is still referenced by: %s; %s",
		entityKind, tag, strings.Join(locators, ", "), hint)
}

func Inbound(tx *gorm.DB, tag string) ([]TagReference, error) {
	rows, err := ssmServiceRows(tx)
	if err != nil {
		return nil, err
	}
	return scanServiceRowsForInboundTag(rows, tag), nil
}

func SSMCascadeServiceIDs(tx *gorm.DB, inboundTag string) ([]uint, error) {
	rows, err := ssmServiceRows(tx)
	if err != nil {
		return nil, err
	}
	return ssmServiceIdsReferencingInbound(rows, inboundTag), nil
}

func ssmServiceRows(tx *gorm.DB) ([]model.Service, error) {
	var rows []model.Service
	err := tx.Model(model.Service{}).Select("id", "type", "tag", "options").
		Where("type = ?", "ssm-api").Find(&rows).Error
	return rows, err
}

// Outbound scans the whole outbound namespace. sing-box resolves
// outbounds and endpoints by tag in one namespace, so both tables must be
// considered together with service detours and base config references.
func Outbound(tx *gorm.DB, tag string, excludeOutboundID uint, excludeEndpointID uint) ([]TagReference, error) {
	var outbounds []model.Outbound
	if err := tx.Model(model.Outbound{}).Select("id", "type", "tag", "options").Find(&outbounds).Error; err != nil {
		return nil, err
	}
	refs := scanOutboundRowsForTag(outbounds, tag, excludeOutboundID)

	var endpoints []model.Endpoint
	if err := tx.Model(model.Endpoint{}).Select("id", "type", "tag", "options").Find(&endpoints).Error; err != nil {
		return nil, err
	}
	refs = append(refs, scanEndpointRowsForTag(endpoints, tag, excludeEndpointID)...)

	var services []model.Service
	if err := tx.Model(model.Service{}).Select("id", "type", "tag", "options").Find(&services).Error; err != nil {
		return nil, err
	}
	refs = append(refs, scanServiceRowsForOutboundDetour(services, tag)...)

	blob, err := configBlobFrom(tx)
	if err != nil {
		return nil, err
	}
	blobRefs, err := scanConfigBlobForOutboundTag(blob, tag)
	if err != nil {
		return nil, err
	}
	return append(refs, blobRefs...), nil
}

func configBlobFrom(tx *gorm.DB) ([]byte, error) {
	var stored model.Setting
	result := tx.Model(model.Setting{}).Where("key = ?", "config").Limit(1).Find(&stored)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return []byte(stored.Value), nil
}
