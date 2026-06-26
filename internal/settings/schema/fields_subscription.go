package schema

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

func subscriptionFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.SubEncodeKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subEncode", Order: 10},
		{Key: settingcatalog.SubShowInfoKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subInfo", Order: 20},
		{Key: settingcatalog.SubSecretRequiredKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subSecretRequired", Order: 30},
		{Key: settingcatalog.SubLinkEnableKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subLinkEnable", Order: 40},
		{Key: settingcatalog.SubJsonEnableKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subJsonEnable", Order: 50},
		{Key: settingcatalog.SubClashEnableKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subClashEnable", Order: 60},
		{Key: settingcatalog.SubXrayEnableKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subXrayEnable", Order: 70},
		{Key: settingcatalog.SubListenKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeString, LabelKey: "setting.addr", Order: 80},
		{Key: settingcatalog.SubPortKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeInt, LabelKey: "setting.port", Min: intPtr(1), Max: intPtr(65535), Order: 90},
		{Key: settingcatalog.SubKeyFileKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypePath, LabelKey: "setting.sslKey", Order: 100},
		{Key: settingcatalog.SubCertFileKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypePath, LabelKey: "setting.sslCert", Order: 110},
		{Key: settingcatalog.SubDomainKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeString, LabelKey: "setting.domain", Order: 120},
		{Key: settingcatalog.SubPathKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypePath, LabelKey: "setting.path", RestartRequired: true, Order: 130},
		{Key: settingcatalog.SubUpdatesKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeInt, LabelKey: "setting.update", Min: intPtr(0), Order: 140},
		{Key: settingcatalog.SubURIKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeURL, LabelKey: "setting.subUri", Order: 150},
		{Key: settingcatalog.SubTitleKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeString, LabelKey: "setting.subTitle", Advanced: true, Order: 160},
		{Key: settingcatalog.SubSupportURLKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeURL, LabelKey: "setting.subSupportUrl", Advanced: true, Order: 170},
		{Key: settingcatalog.SubProfileURLKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeURL, LabelKey: "setting.subProfileUrl", Advanced: true, Order: 180},
		{Key: settingcatalog.SubRateLimitPerIPKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeInt, LabelKey: "setting.subRateLimitPerIP", Min: intPtr(0), Advanced: true, Order: 190},
		{Key: settingcatalog.SubNameInRemarkKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeBool, LabelKey: "setting.subNameInRemark", Advanced: true, Order: 200},
		{Key: settingcatalog.SubAnnounceKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeText, LabelKey: "setting.subAnnounce", Advanced: true, Order: 210},
		{Key: settingcatalog.SubRemoteGroupAdaptationKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeEnum, LabelKey: "setting.subRemoteGroupAdaptation", Options: []string{"urltest", "selector", "failover"}, Advanced: true, Order: 220},
		{Key: settingcatalog.SubRemoteConversionPolicyKey, Page: PageSettings, Group: GroupSubscription, Type: FieldTypeJSON, LabelKey: "setting.subRemoteConversionPolicy", Advanced: true, Order: 230},

		{Key: settingcatalog.SubJsonPathKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypePath, LabelKey: "setting.jsonPath", RestartRequired: true, Order: 10},
		{Key: settingcatalog.SubJsonURIKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypeURL, LabelKey: "setting.jsonSub", Order: 20},
		{Key: settingcatalog.SubJsonFragmentKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypeString, LabelKey: "setting.fragment", Advanced: true, Order: 30},
		{Key: settingcatalog.SubJsonNoisesKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypeJSON, LabelKey: "setting.noises", Advanced: true, Order: 40},
		{Key: settingcatalog.SubJsonMuxKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypeBool, LabelKey: "basic.mux.title", Advanced: true, Order: 50},
		{Key: settingcatalog.SubJsonDirectRulesKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypeBool, LabelKey: "setting.directRules", Advanced: true, Order: 60},
		{Key: settingcatalog.SubJsonExtKey, Page: PageSettings, Group: GroupSubscriptionJSON, Type: FieldTypeJSON, LabelKey: "setting.jsonSubOptions", Advanced: true, Order: 70},

		{Key: settingcatalog.SubClashPathKey, Page: PageSettings, Group: GroupSubscriptionClash, Type: FieldTypePath, LabelKey: "setting.clashPath", RestartRequired: true, Order: 10},
		{Key: settingcatalog.SubClashURIKey, Page: PageSettings, Group: GroupSubscriptionClash, Type: FieldTypeURL, LabelKey: "setting.clashSub", Order: 20},
		{Key: settingcatalog.SubClashExtKey, Page: PageSettings, Group: GroupSubscriptionClash, Type: FieldTypeYAML, LabelKey: "setting.clashSub", Advanced: true, Order: 30},

		{Key: settingcatalog.SubXrayPathKey, Page: PageSettings, Group: GroupSubscriptionXray, Type: FieldTypePath, LabelKey: "setting.xrayPath", RestartRequired: true, Order: 10},
		{Key: settingcatalog.SubXrayURIKey, Page: PageSettings, Group: GroupSubscriptionXray, Type: FieldTypeURL, LabelKey: "setting.xraySub", Order: 20},
	}
}
