package service

const (
	settingKeySubListen          = "subListen"
	settingKeySubPort            = "subPort"
	settingKeySubPath            = "subPath"
	settingKeySubDomain          = "subDomain"
	settingKeySubCertFile        = "subCertFile"
	settingKeySubKeyFile         = "subKeyFile"
	settingKeySubUpdates         = "subUpdates"
	settingKeySubEncode          = "subEncode"
	settingKeySubShowInfo        = "subShowInfo"
	settingKeySubSecretRequired  = "subSecretRequired"
	settingKeySubRateLimitPerIP  = "subRateLimitPerIP"
	settingKeySubLinkEnable      = "subLinkEnable"
	settingKeySubJsonEnable      = "subJsonEnable"
	settingKeySubClashEnable     = "subClashEnable"
	settingKeySubJsonPath        = "subJsonPath"
	settingKeySubClashPath       = "subClashPath"
	settingKeySubJsonURI         = "subJsonURI"
	settingKeySubClashURI        = "subClashURI"
	settingKeySubTitle           = "subTitle"
	settingKeySubSupportURL      = "subSupportUrl"
	settingKeySubProfileURL      = "subProfileUrl"
	settingKeySubAnnounce        = "subAnnounce"
	settingKeySubNameInRemark    = "subNameInRemark"
	settingKeySubJsonFragment    = "subJsonFragment"
	settingKeySubJsonNoises      = "subJsonNoises"
	settingKeySubJsonMux         = "subJsonMux"
	settingKeySubJsonDirectRules = "subJsonDirectRules"
	settingKeySubURI             = "subURI"
	settingKeySubJsonExt         = "subJsonExt"
	settingKeySubClashExt        = "subClashExt"
)

var defaultSubscriptionSettingValues = map[string]string{
	settingKeySubListen:          "",
	settingKeySubPort:            "2096",
	settingKeySubPath:            "/sub/",
	settingKeySubDomain:          "",
	settingKeySubCertFile:        "",
	settingKeySubKeyFile:         "",
	settingKeySubUpdates:         "12",
	settingKeySubEncode:          "true",
	settingKeySubShowInfo:        "false",
	settingKeySubSecretRequired:  "false",
	settingKeySubRateLimitPerIP:  "60",
	settingKeySubLinkEnable:      "true",
	settingKeySubJsonEnable:      "true",
	settingKeySubClashEnable:     "true",
	settingKeySubJsonPath:        "/json/",
	settingKeySubClashPath:       "/clash/",
	settingKeySubJsonURI:         "",
	settingKeySubClashURI:        "",
	settingKeySubTitle:           "",
	settingKeySubSupportURL:      "",
	settingKeySubProfileURL:      "",
	settingKeySubAnnounce:        "",
	settingKeySubNameInRemark:    "false",
	settingKeySubJsonFragment:    "",
	settingKeySubJsonNoises:      "",
	settingKeySubJsonMux:         "false",
	settingKeySubJsonDirectRules: "false",
	settingKeySubURI:             "",
	settingKeySubJsonExt:         "",
	settingKeySubClashExt:        "",
}

var subscriptionPathSettingKeys = []string{
	settingKeySubPath,
	settingKeySubJsonPath,
	settingKeySubClashPath,
}

var subscriptionBooleanSettingKeys = settingKeySet(
	settingKeySubLinkEnable,
	settingKeySubJsonEnable,
	settingKeySubClashEnable,
	settingKeySubNameInRemark,
	settingKeySubJsonMux,
	settingKeySubJsonDirectRules,
)

var subscriptionURLSettingKeys = settingKeySet(
	settingKeySubJsonURI,
	settingKeySubClashURI,
	settingKeySubSupportURL,
	settingKeySubProfileURL,
)
