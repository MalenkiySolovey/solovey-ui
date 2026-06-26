package service

import settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"

func (s *SettingService) GetSettingSchema() []settingsschema.Field {
	return settingsSchema.PublicFields()
}

func (s *SettingService) GetAllSettingSchema() []settingsschema.Field {
	return settingsSchema.Fields()
}
