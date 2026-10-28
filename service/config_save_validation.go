package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// Native inbound/outbound credentials and inbound groups require a complete candidate: their
// Users, TLS and member owners supply fields that an isolated inbound draft
// cannot validate. Construct and close with the serving registry before the existing
// save transaction commits. Dry validation starts no listener or runtime.
func (s *ConfigService) validateNativeInboundCandidate(tx *gorm.DB) error {
	var kinds []string
	for _, fact := range clientfacts.Facts() {
		if fact.SecretField != "" && entitycapabilities.Resolve("inbounds", fact.Type).Available {
			kinds = append(kinds, fact.Type)
		}
	}
	for _, kind := range []string{"bond", "core-failover", "call"} {
		if entitycapabilities.Resolve("inbounds", kind).Available {
			kinds = append(kinds, kind)
		}
	}
	var count int64
	if len(kinds) > 0 && tx.Migrator().HasTable(&model.Inbound{}) {
		if err := tx.Model(&model.Inbound{}).Where("type IN ?", kinds).Count(&count).Error; err != nil {
			return err
		}
	}
	var outboundCount int64
	var outboundKinds []string
	for _, kind := range []string{"call", "mieru", "trusttunnel", "sudoku", "masque", "bond", "core-failover"} {
		if entitycapabilities.Resolve("outbounds", kind).Available {
			outboundKinds = append(outboundKinds, kind)
		}
	}
	if len(outboundKinds) != 0 && tx.Migrator().HasTable(&model.Outbound{}) {
		if err := tx.Model(&model.Outbound{}).Where("type IN ?", outboundKinds).Count(&outboundCount).Error; err != nil {
			return err
		}
	}
	transportCandidate := false
	if registry.ResolveTransport("xhttp").Available() || registry.ResolveTransport("mkcp").Available() || registry.ResolveTransport("quic").Available() {
		for _, entity := range []any{&model.Inbound{}, &model.Outbound{}} {
			if !tx.Migrator().HasTable(entity) {
				continue
			}
			var rows []struct{ Options json.RawMessage }
			if err := tx.Model(entity).Select("options").Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				var options struct{ Transport struct{ Type string } }
				if json.Unmarshal(row.Options, &options) == nil && (options.Transport.Type == "xhttp" || options.Transport.Type == "mkcp" || options.Transport.Type == "quic") && registry.ResolveTransport(options.Transport.Type).Available() {
					transportCandidate = true
					break
				}
			}
		}
	}
	if count == 0 && outboundCount == 0 && !transportCandidate {
		return nil
	}
	projection, err := s.singBoxConfigBuilder().BuildCandidateProjectionFromDB(tx, "", false)
	if err == nil {
		err = singboxvalidation.ValidateConfig(projection.Config)
	}
	if err != nil {
		// Provider diagnostics may quote configuration values. Return only an
		// owner code; the rejected candidate and its credential uses roll back.
		if count == 0 {
			return common.NewError("NATIVE_OUTBOUND_CANDIDATE_REJECTED: the complete provider configuration is invalid")
		}
		return common.NewError("NATIVE_INBOUND_CANDIDATE_REJECTED: the complete provider configuration is invalid")
	}
	return nil
}
