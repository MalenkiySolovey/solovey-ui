package validation

import "testing"

func TestValidateIntRange(t *testing.T) {
	if err := ValidateIntRange("limit", "5", 1, 10); err != nil {
		t.Fatalf("valid int range returned error: %v", err)
	}
	for _, value := range []string{"0", "11", "bad"} {
		if err := ValidateIntRange("limit", value, 1, 10); err == nil {
			t.Fatalf("ValidateIntRange(%q) succeeded, want error", value)
		}
	}
}

func TestValidateTransportMode(t *testing.T) {
	for _, value := range []string{"proxy", "outbound"} {
		if err := ValidateTransportMode(value); err != nil {
			t.Fatalf("ValidateTransportMode(%q) returned error: %v", value, err)
		}
	}
	if err := ValidateTransportMode("direct"); err == nil {
		t.Fatal("invalid transport mode succeeded")
	}
}
