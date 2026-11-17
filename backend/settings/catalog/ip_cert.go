package catalog

const (
	IPCertEnabledKey             = "ipCertEnabled"
	IPCertTargetIPKey            = "ipCertTargetIP"
	IPCertEmailKey               = "ipCertEmail"
	IPCertChallengePortKey       = "ipCertChallengePort"
	IPCertApplyTargetKey         = "ipCertApplyTarget"
	IPCertAccountKeyKey          = "ipCertAccountKey"
	IPCertAccountRegistrationKey = "ipCertAccountRegistration"
	IPCertLastIPKey              = "ipCertLastIP"
	IPCertCertPathKey            = "ipCertCertPath"
	IPCertKeyPathKey             = "ipCertKeyPath"
	IPCertNotAfterKey            = "ipCertNotAfter"
	IPCertLastIssueKey           = "ipCertLastIssue"
)

func IPCertDefaults() map[string]string {
	return map[string]string{
		IPCertEnabledKey:             "false",
		IPCertTargetIPKey:            "",
		IPCertEmailKey:               "",
		IPCertChallengePortKey:       "80",
		IPCertApplyTargetKey:         "panel",
		IPCertAccountKeyKey:          "",
		IPCertAccountRegistrationKey: "",
		IPCertLastIPKey:              "",
		IPCertCertPathKey:            "",
		IPCertKeyPathKey:             "",
		IPCertNotAfterKey:            "",
		IPCertLastIssueKey:           "",
	}
}

func IPCertInternalKeys() []string {
	return []string{
		IPCertAccountKeyKey,
		IPCertAccountRegistrationKey,
		IPCertLastIPKey,
		IPCertCertPathKey,
		IPCertKeyPathKey,
		IPCertNotAfterKey,
		IPCertLastIssueKey,
	}
}

func IPCertInternalKeySet() map[string]struct{} {
	return KeySet(IPCertInternalKeys()...)
}

func IPCertEncryptedKeys() map[string]struct{} {
	return KeySet(IPCertAccountKeyKey)
}
