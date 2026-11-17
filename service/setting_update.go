package service

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-backend/settings/catalog"
	configupdate "github.com/MalenkiySolovey/solovey-ui/config/update"
)

func (s *SettingService) GetUpdateChannel() string {
	value, err := s.getString(settingcatalog.UpdateChannelKey)
	if err != nil {
		return configupdate.ChannelMain
	}
	return configupdate.NormalizeChannel(value)
}

func (s *SettingService) SetUpdateChannel(channel string) error {
	return s.setString(settingcatalog.UpdateChannelKey, configupdate.NormalizeChannel(channel))
}
