package events

import "testing"

func TestRetentionPolicyValidation(t *testing.T) {
	if err := (RetentionPolicy{GlobalLimit: 1000, PerResourceLimit: 200}).Validate(); err != nil {
		t.Fatalf("valid retention policy: %v", err)
	}
	if err := (RetentionPolicy{GlobalLimit: 100, PerResourceLimit: 200}).Validate(); err == nil {
		t.Fatalf("per-resource limit above global limit was accepted")
	}
}
