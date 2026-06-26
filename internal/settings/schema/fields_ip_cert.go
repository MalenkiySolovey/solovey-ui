package schema

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

func ipCertFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.IPCertEnabledKey, Page: PageIPCert, Group: GroupIPCertPublic, Type: FieldTypeBool, LabelKey: "ipCert.enabled", Order: 10},
		{Key: settingcatalog.IPCertTargetIPKey, Page: PageIPCert, Group: GroupIPCertPublic, Type: FieldTypeString, LabelKey: "ipCert.targetIp", Order: 20},
		{Key: settingcatalog.IPCertEmailKey, Page: PageIPCert, Group: GroupIPCertPublic, Type: FieldTypeString, LabelKey: "ipCert.email", Order: 30},
		{Key: settingcatalog.IPCertChallengePortKey, Page: PageIPCert, Group: GroupIPCertPublic, Type: FieldTypeInt, LabelKey: "ipCert.challengePort", Min: intPtr(1), Max: intPtr(65535), Order: 40},
		{Key: settingcatalog.IPCertApplyTargetKey, Page: PageIPCert, Group: GroupIPCertPublic, Type: FieldTypeEnum, LabelKey: "ipCert.applyTarget", Options: []string{"panel", "subscription", "both"}, Order: 50},

		{Key: settingcatalog.IPCertAccountKeyKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypeSecret, LabelKey: "ipCert.accountKey", Order: 10},
		{Key: settingcatalog.IPCertAccountRegistrationKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypeJSON, LabelKey: "ipCert.accountRegistration", Order: 20},
		{Key: settingcatalog.IPCertLastIPKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypeString, LabelKey: "ipCert.lastIp", Order: 30},
		{Key: settingcatalog.IPCertCertPathKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypePath, LabelKey: "ipCert.certPath", Order: 40},
		{Key: settingcatalog.IPCertKeyPathKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypePath, LabelKey: "ipCert.keyPath", Order: 50},
		{Key: settingcatalog.IPCertNotAfterKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypeString, LabelKey: "ipCert.notAfter", Order: 60},
		{Key: settingcatalog.IPCertLastIssueKey, Page: PageIPCert, Group: GroupIPCertInternal, Type: FieldTypeString, LabelKey: "ipCert.lastIssue", Order: 70},
	}
}
