package catalog

import (
	"reflect"
	"testing"
)

func TestIPCertDefaults(t *testing.T) {
	defaults := IPCertDefaults()
	if defaults[IPCertEnabledKey] != "false" {
		t.Fatalf("ip cert enabled default = %q", defaults[IPCertEnabledKey])
	}
	if defaults[IPCertChallengePortKey] != "80" {
		t.Fatalf("ip cert challenge port default = %q", defaults[IPCertChallengePortKey])
	}
	if defaults[IPCertApplyTargetKey] != "panel" {
		t.Fatalf("ip cert apply target default = %q", defaults[IPCertApplyTargetKey])
	}
}

func TestIPCertKeyGroups(t *testing.T) {
	wantInternal := []string{
		IPCertAccountKeyKey,
		IPCertAccountRegistrationKey,
		IPCertLastIPKey,
		IPCertCertPathKey,
		IPCertKeyPathKey,
		IPCertNotAfterKey,
		IPCertLastIssueKey,
	}
	if got := IPCertInternalKeys(); !reflect.DeepEqual(got, wantInternal) {
		t.Fatalf("internal keys = %#v", got)
	}
	if _, ok := IPCertInternalKeySet()[IPCertCertPathKey]; !ok {
		t.Fatal("cert path should be internal")
	}
	if _, ok := IPCertEncryptedKeys()[IPCertAccountKeyKey]; !ok {
		t.Fatal("account key should be encrypted")
	}
	if _, ok := IPCertEncryptedKeys()[IPCertAccountRegistrationKey]; ok {
		t.Fatal("account registration should not be encrypted")
	}
}
