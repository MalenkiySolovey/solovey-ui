package tracker

import (
	"os"
	"strings"
	"testing"
)

func TestTrackerPolicyMatchesSingBoxDependency(t *testing.T) {
	version := requiredModuleVersion(t, "../../go.mod", TrackerValidatedSingBoxModule)
	if TrackerValidatedSingBoxVersion != version {
		t.Fatalf("%s bumped from %s to %s; revalidate trackers and update %s",
			TrackerValidatedSingBoxModule,
			TrackerValidatedSingBoxVersion,
			version,
			TrackerRevalidationPolicyName,
		)
	}

	status := SingBoxTrackerRevalidationStatus(version)
	if status.Required {
		t.Fatalf("current sing-box version should be covered by tracker policy: %#v", status)
	}
	if len(status.Checks) == 0 {
		t.Fatal("tracker revalidation policy must include explicit checks")
	}
}

func TestTrackerPolicyRequiresRevalidationOnVersionChange(t *testing.T) {
	status := SingBoxTrackerRevalidationStatus("v99.0.0")
	if !status.Required {
		t.Fatal("unexpectedly accepted unvalidated sing-box version")
	}
}

func TestTrackerPolicyStatusCoversCurrentChecklist(t *testing.T) {
	status := SingBoxTrackerRevalidationStatus(TrackerValidatedSingBoxVersion)
	if status.PolicyName != TrackerRevalidationPolicyName {
		t.Fatalf("policy name = %q, want %q", status.PolicyName, TrackerRevalidationPolicyName)
	}
	if status.ValidatedVersion != TrackerValidatedSingBoxVersion {
		t.Fatalf("validated version = %q, want %q", status.ValidatedVersion, TrackerValidatedSingBoxVersion)
	}
	seen := make(map[string]bool, len(status.Checks))
	for _, check := range status.Checks {
		seen[check] = true
	}
	for _, check := range TrackerRevalidationChecks {
		if !seen[check] {
			t.Fatalf("policy status missing tracker check %q", check)
		}
	}
}

func requiredModuleVersion(t *testing.T, path string, module string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == module {
			return fields[1]
		}
	}
	t.Fatalf("module %s not found in %s", module, path)
	return ""
}
