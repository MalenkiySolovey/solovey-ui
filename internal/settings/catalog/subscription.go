package catalog

import subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"

const (
	SubListenKey                = "subListen"
	SubPortKey                  = "subPort"
	SubPathKey                  = "subPath"
	SubDomainKey                = "subDomain"
	SubCertFileKey              = "subCertFile"
	SubKeyFileKey               = "subKeyFile"
	SubUpdatesKey               = "subUpdates"
	SubEncodeKey                = "subEncode"
	SubShowInfoKey              = "subShowInfo"
	SubSecretRequiredKey        = "subSecretRequired"
	SubRateLimitPerIPKey        = "subRateLimitPerIP"
	SubLinkEnableKey            = "subLinkEnable"
	SubJsonEnableKey            = "subJsonEnable"
	SubClashEnableKey           = "subClashEnable"
	SubXrayEnableKey            = "subXrayEnable"
	SubRemoteGroupAdaptationKey = "subRemoteGroupAdaptation"
	SubRemoteConversionPolicyKey = "subRemoteConversionPolicy"
	SubJsonPathKey              = "subJsonPath"
	SubClashPathKey             = "subClashPath"
	SubXrayPathKey              = "subXrayPath"
	SubJsonURIKey               = "subJsonURI"
	SubClashURIKey              = "subClashURI"
	SubXrayURIKey               = "subXrayURI"
	SubTitleKey                 = "subTitle"
	SubSupportURLKey            = "subSupportUrl"
	SubProfileURLKey            = "subProfileUrl"
	SubAnnounceKey              = "subAnnounce"
	SubNameInRemarkKey          = "subNameInRemark"
	SubJsonFragmentKey          = "subJsonFragment"
	SubJsonNoisesKey            = "subJsonNoises"
	SubJsonMuxKey               = "subJsonMux"
	SubJsonDirectRulesKey       = "subJsonDirectRules"
	SubURIKey                   = "subURI"
	SubJsonExtKey               = "subJsonExt"
	SubClashExtKey              = "subClashExt"
)

func SubscriptionDefaults() map[string]string {
	return map[string]string{
		SubListenKey:                "",
		SubPortKey:                  "2096",
		SubPathKey:                  "/sub/",
		SubDomainKey:                "",
		SubCertFileKey:              "",
		SubKeyFileKey:               "",
		SubUpdatesKey:               "12",
		SubEncodeKey:                "true",
		SubShowInfoKey:              "false",
		SubSecretRequiredKey:        "false",
		SubRateLimitPerIPKey:        "60",
		SubLinkEnableKey:            "true",
		SubJsonEnableKey:            "true",
		SubClashEnableKey:           "true",
		SubXrayEnableKey:            "true",
		SubRemoteGroupAdaptationKey: "urltest",
		SubRemoteConversionPolicyKey: subconversion.DefaultPolicyJSON(),
		SubJsonPathKey:              "/json/",
		SubClashPathKey:             "/clash/",
		SubXrayPathKey:              "/xray/",
		SubJsonURIKey:               "",
		SubClashURIKey:              "",
		SubXrayURIKey:               "",
		SubTitleKey:                 "",
		SubSupportURLKey:            "",
		SubProfileURLKey:            "",
		SubAnnounceKey:              "",
		SubNameInRemarkKey:          "false",
		SubJsonFragmentKey:          "",
		SubJsonNoisesKey:            "",
		SubJsonMuxKey:               "false",
		SubJsonDirectRulesKey:       "false",
		SubURIKey:                   "",
		SubJsonExtKey:               "",
		SubClashExtKey:              "",
	}
}

func SubscriptionPathKeys() []string {
	return []string{
		SubPathKey,
		SubJsonPathKey,
		SubClashPathKey,
		SubXrayPathKey,
	}
}

func SubscriptionBooleanKeys() map[string]struct{} {
	return KeySet(
		SubLinkEnableKey,
		SubJsonEnableKey,
		SubClashEnableKey,
		SubXrayEnableKey,
		SubNameInRemarkKey,
		SubJsonMuxKey,
		SubJsonDirectRulesKey,
	)
}

func SubscriptionURLKeys() map[string]struct{} {
	return KeySet(
		SubJsonURIKey,
		SubClashURIKey,
		SubXrayURIKey,
		SubSupportURLKey,
		SubProfileURLKey,
	)
}
