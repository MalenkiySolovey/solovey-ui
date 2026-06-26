package service

import (
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
)

var defaultValueMap = settingcatalog.MergeDefaultMaps(
	defaultWebSettingValues,
	defaultSessionSettingValues,
	defaultRuntimeSettingValues,
	defaultSubscriptionSettingValues,
	defaultTelegramSettingValues,
	defaultPaidSubSettingValues,
	defaultIpCertSettingValues,
	defaultInternalSettingValues,
)

var internalSettingKeys = settingcatalog.MergeKeySets(
	settingKeySet(
		settingcatalog.SecretKey,
		settingcatalog.InstallSaltKey,
		settingcatalog.SessionGenerationKey,
		settingcatalog.ConfigKey,
		settingcatalog.VersionKey,
		settingKeyPaidSubUpdateOffset,
	),
	ipCertInternalSettingKeySet,
)

var settingsSchema = settingsschema.New(defaultValueMap, internalSettingKeys, encryptedSettingKeys, settingsschema.DefaultFieldMetadata()...)

func defaultSettingValue(key string) (string, bool) {
	value, ok := settingsSchema.Default(key)
	if !ok {
		return "", false
	}
	if key == settingcatalog.VersionKey {
		return configidentity.GetVersion(), true
	}
	return value, true
}

func settingKeySet(keys ...string) map[string]struct{} {
	return settingcatalog.KeySet(keys...)
}
