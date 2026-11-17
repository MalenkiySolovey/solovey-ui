package service

import settingsschema "github.com/MalenkiySolovey/solovey-ui/backend/settings/schema"

func (s *SettingService) GetSettingSchema() []settingsschema.Field {
	return currentSettingsSchema().PublicFields()
}

func (s *SettingService) GetAllSettingSchema() []settingsschema.Field {
	return currentSettingsSchema().Fields()
}
