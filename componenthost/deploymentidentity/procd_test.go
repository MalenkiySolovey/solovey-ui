package deploymentidentity

import (
	"strings"
	"testing"
)

func TestProcdApplicationOwnerContractRejectsRevisionDrift(t *testing.T) {
	contract, err := NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/releases/current/solovey-ui", strings.Repeat("6", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	contract.ProcdInstance = "drift"
	if err := contract.Validate(); err == nil {
		t.Fatal("changed procd instance retained a valid contract revision")
	}
}
