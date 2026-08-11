package response

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSNIPrereadDowngradeHasNoLeaseTopologyOrAppliedActionSurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "resolver.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `SNIPrereadActionMapDecisionV2 = "DOWNGRADE_ONLY"`) || !strings.Contains(text, "ForcedSameSubjectDecoyRoute: false") {
		t.Fatal("SNI downgrade capability is not explicit")
	}
	for _, forbidden := range []string{"EndpointLeaseProvider", "ProviderTargetReservation", "PrepareV2", "ApplyV2", "AppliedActionV1{"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("downgrade-only resolver gained mutation/action evidence surface %q", forbidden)
		}
	}
}
