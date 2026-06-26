package service

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type PanelLoadSettings struct {
	Config      string
	SubURI      string
	SubJsonURI  string
	SubClashURI string
	SubXrayURI  string
	TrafficAge  int
}

func (s *SettingService) LoadPanelSettingsForData(host string) (PanelLoadSettings, error) {
	keys := []string{
		settingcatalog.ConfigKey,
		settingKeySubURI,
		settingKeySubKeyFile,
		settingKeySubCertFile,
		settingKeySubDomain,
		settingKeySubPort,
		settingKeySubPath,
		settingKeySubJsonURI,
		settingKeySubClashURI,
		settingKeySubXrayURI,
		settingcatalog.TrafficAgeKey,
	}
	values, err := s.getSettingsSnapshot(keys...)
	if err != nil {
		return PanelLoadSettings{}, err
	}
	trafficAge, err := strconv.Atoi(values[settingcatalog.TrafficAgeKey])
	if err != nil {
		return PanelLoadSettings{}, err
	}
	endpoint := subscriptionEndpointSettings{
		OverrideURI: values[settingKeySubURI],
		Domain:      values[settingKeySubDomain],
		Port:        values[settingKeySubPort],
		CertFile:    values[settingKeySubCertFile],
		KeyFile:     values[settingKeySubKeyFile],
		Path:        settingsvalidation.NormalizeURLPath(values[settingKeySubPath]),
	}
	subURI := endpoint.OverrideURI
	if subURI == "" {
		subURI = endpoint.BaseURI(host)
	}
	return PanelLoadSettings{
		Config:      values[settingcatalog.ConfigKey],
		SubURI:      subURI,
		SubJsonURI:  values[settingKeySubJsonURI],
		SubClashURI: values[settingKeySubClashURI],
		SubXrayURI:  values[settingKeySubXrayURI],
		TrafficAge:  trafficAge,
	}, nil
}

func (s *SettingService) getSettingsSnapshot(keys ...string) (map[string]string, error) {
	db := settingsDatabase()
	if db == nil {
		return nil, common.NewError("database is not initialized")
	}
	settings := make([]model.Setting, 0, len(keys))
	if err := db.Model(model.Setting{}).Where("key IN ?", keys).Find(&settings).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, ok := defaultSettingValue(key)
		if !ok {
			return nil, common.NewErrorf("key <%v> not in defaultValueMap", key)
		}
		values[key] = value
	}
	for _, setting := range settings {
		if settingsSchema.Encrypted(setting.Key) {
			value, err := s.decryptSettingValue(setting.Key, setting.Value)
			if err != nil {
				return nil, err
			}
			values[setting.Key] = value
			continue
		}
		values[setting.Key] = setting.Value
	}
	return values, nil
}
