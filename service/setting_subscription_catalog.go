package service

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

const (
	settingKeySubListen                 = settingcatalog.SubListenKey
	settingKeySubPort                   = settingcatalog.SubPortKey
	settingKeySubPath                   = settingcatalog.SubPathKey
	settingKeySubDomain                 = settingcatalog.SubDomainKey
	settingKeySubCertFile               = settingcatalog.SubCertFileKey
	settingKeySubKeyFile                = settingcatalog.SubKeyFileKey
	settingKeySubUpdates                = settingcatalog.SubUpdatesKey
	settingKeySubEncode                 = settingcatalog.SubEncodeKey
	settingKeySubShowInfo               = settingcatalog.SubShowInfoKey
	settingKeySubSecretRequired         = settingcatalog.SubSecretRequiredKey
	settingKeySubRateLimitPerIP         = settingcatalog.SubRateLimitPerIPKey
	settingKeySubLinkEnable             = settingcatalog.SubLinkEnableKey
	settingKeySubJsonEnable             = settingcatalog.SubJsonEnableKey
	settingKeySubClashEnable            = settingcatalog.SubClashEnableKey
	settingKeySubXrayEnable             = settingcatalog.SubXrayEnableKey
	settingKeySubRemoteGroupAdaptation  = settingcatalog.SubRemoteGroupAdaptationKey
	settingKeySubRemoteConversionPolicy = settingcatalog.SubRemoteConversionPolicyKey
	settingKeySubJsonPath               = settingcatalog.SubJsonPathKey
	settingKeySubClashPath              = settingcatalog.SubClashPathKey
	settingKeySubXrayPath               = settingcatalog.SubXrayPathKey
	settingKeySubJsonURI                = settingcatalog.SubJsonURIKey
	settingKeySubClashURI               = settingcatalog.SubClashURIKey
	settingKeySubXrayURI                = settingcatalog.SubXrayURIKey
	settingKeySubTitle                  = settingcatalog.SubTitleKey
	settingKeySubSupportURL             = settingcatalog.SubSupportURLKey
	settingKeySubProfileURL             = settingcatalog.SubProfileURLKey
	settingKeySubAnnounce               = settingcatalog.SubAnnounceKey
	settingKeySubNameInRemark           = settingcatalog.SubNameInRemarkKey
	settingKeySubJsonFragment           = settingcatalog.SubJsonFragmentKey
	settingKeySubJsonNoises             = settingcatalog.SubJsonNoisesKey
	settingKeySubJsonMux                = settingcatalog.SubJsonMuxKey
	settingKeySubJsonDirectRules        = settingcatalog.SubJsonDirectRulesKey
	settingKeySubURI                    = settingcatalog.SubURIKey
	settingKeySubJsonExt                = settingcatalog.SubJsonExtKey
	settingKeySubClashExt               = settingcatalog.SubClashExtKey
)

var defaultSubscriptionSettingValues = settingcatalog.SubscriptionDefaults()

var subscriptionPathSettingKeys = settingcatalog.SubscriptionPathKeys()
