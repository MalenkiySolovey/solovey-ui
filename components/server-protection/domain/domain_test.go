package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalEnumsRejectUnknownValues(t *testing.T) {
	validators := []func() error{
		func() error { return ProfileMode("unknown").Validate() },
		func() error { return SignalKind("unknown").Validate() },
		func() error { return DecisionAction("unknown").Validate() },
		func() error { return ResourceKind("unknown").Validate() },
		func() error { return Protocol("unknown").Validate() },
		func() error { return OperationState("unknown").Validate() },
		func() error { return FirewallBackend("unknown").Validate() },
		func() error { return SupportState("unknown").Validate() },
	}
	for index, validate := range validators {
		if err := validate(); err == nil {
			t.Fatalf("validator %d accepted an unknown value", index)
		}
	}
}

func TestSafeMetaBoundsAndRejectsSensitiveShapes(t *testing.T) {
	meta := SafeMeta{
		PathClass:               strings.Repeat("a", 256),
		UAClass:                 strings.Repeat("b", 256),
		ClassifierPolicyVersion: ClassifierPolicyVersion,
	}.Bounded(128)
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(encoded) > 128 || !meta.Truncated {
		t.Fatalf("bounded safe meta = %d bytes, truncated=%v: %s", len(encoded), meta.Truncated, encoded)
	}
	unsafe := SafeMeta{PathClass: "admin?token=secret", ClassifierPolicyVersion: ClassifierPolicyVersion}
	if err := unsafe.Validate(); err == nil {
		t.Fatalf("sensitive-looking classifier value was accepted")
	}
}
