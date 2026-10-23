package updatebroker

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
)

func providerMaintenanceFixture(t *testing.T) ProviderMaintenanceV1 {
	t.Helper()
	return ProviderMaintenanceV1{ConfigurationDigest: updateDigest("configuration"), ConfigurationBytes: 2,
		ConfigurationRevision: updateDigest("desired-revision"), BackupRef: updateDigest("database-backup"),
		PreviousProvider: providerReleaseFixture(t, "sing-box").CoreProvider, PreviousExecutableDigest: updateDigest("previous-executable"),
		RuntimeGeneration: "00000000-0000-4000-8000-000000000001"}
}

func TestProviderMaintenanceCheckpointHasBoundedOwnerEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*ProviderMaintenanceV1){
		"missing-config-digest":       func(v *ProviderMaintenanceV1) { v.ConfigurationDigest = "" },
		"missing-config-revision":     func(v *ProviderMaintenanceV1) { v.ConfigurationRevision = "" },
		"empty-config":                func(v *ProviderMaintenanceV1) { v.ConfigurationBytes = 0 },
		"oversized-config":            func(v *ProviderMaintenanceV1) { v.ConfigurationBytes = targetcheck.MaxConfigBytes + 1 },
		"missing-backup":              func(v *ProviderMaintenanceV1) { v.BackupRef = "" },
		"missing-previous-executable": func(v *ProviderMaintenanceV1) { v.PreviousExecutableDigest = "" },
		"unknown-previous-provider":   func(v *ProviderMaintenanceV1) { v.PreviousProvider.ID = "credential-sentinel" },
		"invalid-generation":          func(v *ProviderMaintenanceV1) { v.RuntimeGeneration = "../credential-sentinel" },
	} {
		t.Run(name, func(t *testing.T) {
			value := providerMaintenanceFixture(t)
			if ValidateProviderMaintenance(value) != nil {
				t.Fatal("valid maintenance evidence rejected")
			}
			mutate(&value)
			if err := ValidateProviderMaintenance(value); err == nil || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatal("unbounded, incomplete or unknown checkpoint was accepted or echoed")
			}
		})
	}
}
