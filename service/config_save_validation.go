package service

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// Credential-backed native inbounds require a complete candidate: the Users
// assignment and TLS owners supply fields that an isolated inbound draft cannot
// validate. Construct and close with the serving registry before the existing
// save transaction commits. Dry validation starts no listener or runtime.
func (s *ConfigService) validateNativeInboundCandidate(tx *gorm.DB) error {
	var kinds []string
	for _, fact := range clientfacts.Facts() {
		if fact.SecretField != "" && entitycapabilities.Resolve("inbounds", fact.Type).Available {
			kinds = append(kinds, fact.Type)
		}
	}
	if len(kinds) == 0 || !tx.Migrator().HasTable(&model.Inbound{}) {
		return nil
	}
	var count int64
	if err := tx.Model(&model.Inbound{}).Where("type IN ?", kinds).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	projection, err := s.singBoxConfigBuilder().BuildCandidateProjectionFromDB(tx, "", false)
	if err == nil {
		err = singboxvalidation.ValidateConfig(projection.Config)
	}
	if err != nil {
		// Provider diagnostics may quote configuration values. Return only an
		// owner code; the rejected candidate and its credential uses roll back.
		return common.NewError("NATIVE_INBOUND_CANDIDATE_REJECTED: the complete provider configuration is invalid")
	}
	return nil
}
