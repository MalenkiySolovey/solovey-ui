package service

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

const (
	settingKeyIpCertEnabled             = settingcatalog.IPCertEnabledKey
	settingKeyIpCertTargetIP            = settingcatalog.IPCertTargetIPKey
	settingKeyIpCertEmail               = settingcatalog.IPCertEmailKey
	settingKeyIpCertChallengePort       = settingcatalog.IPCertChallengePortKey
	settingKeyIpCertApplyTarget         = settingcatalog.IPCertApplyTargetKey
	settingKeyIpCertAccountKey          = settingcatalog.IPCertAccountKeyKey
	settingKeyIpCertAccountRegistration = settingcatalog.IPCertAccountRegistrationKey
	settingKeyIpCertLastIP              = settingcatalog.IPCertLastIPKey
	settingKeyIpCertCertPath            = settingcatalog.IPCertCertPathKey
	settingKeyIpCertKeyPath             = settingcatalog.IPCertKeyPathKey
	settingKeyIpCertNotAfter            = settingcatalog.IPCertNotAfterKey
	settingKeyIpCertLastIssue           = settingcatalog.IPCertLastIssueKey
)

var defaultIpCertSettingValues = settingcatalog.IPCertDefaults()

var ipCertInternalSettingKeys = settingcatalog.IPCertInternalKeys()

var ipCertInternalSettingKeySet = settingcatalog.IPCertInternalKeySet()

var ipCertEncryptedSettingKeys = settingcatalog.IPCertEncryptedKeys()
