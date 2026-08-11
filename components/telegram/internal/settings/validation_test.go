//go:build !minimal

package settings

import "testing"

func TestValidateRejectsInvalidBooleanSettings(t *testing.T) {
	for _, key := range []string{EnabledKey, BackupEnabledKey, NotifyCPUKey, ReportKey} {
		if err := Validate(key, "not-a-bool", "stored"); err == nil {
			t.Fatalf("Validate(%q) accepted an invalid boolean", key)
		}
	}
}

func TestValidateUsesSharedCronContract(t *testing.T) {
	if err := Validate(ReportCronKey, "*/1 * * * *", "stored"); err != nil {
		t.Fatalf("minute schedule rejected: %v", err)
	}
	if err := Validate(BackupCronKey, "invalid cron", "stored"); err == nil {
		t.Fatal("invalid cron schedule accepted")
	}
}
