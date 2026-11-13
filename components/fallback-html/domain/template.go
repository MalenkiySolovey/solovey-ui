//go:build !minimal

package domain

import (
	"gorm.io/gorm"
	"time"
)

func SeedBuiltInTemplateSources(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	now := time.Now().Unix()
	for _, definition := range BuiltInTemplates() {
		source := TemplateSource{TemplateID: definition.ID}
		if err := db.Where("template_id = ?", definition.ID).Assign(TemplateSource{
			TemplateID:         definition.ID,
			Name:               definition.Name,
			Source:             definition.Source,
			License:            definition.License,
			ContentTypeProfile: definition.ContentTypeProfile,
			Installed:          true,
			CreatedAt:          now,
			UpdatedAt:          now,
		}).FirstOrCreate(&source).Error; err != nil {
			return err
		}
	}
	return nil
}
