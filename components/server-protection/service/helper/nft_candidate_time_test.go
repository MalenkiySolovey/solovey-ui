package helper

import (
	"strings"
	"testing"
	"time"
)

func TestDatedCandidateConsumesExistingAbsoluteExpiration(t *testing.T) {
	now := time.Unix(2000, 0)
	raw := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + strings.Repeat("a", 64) + "\"\n set blocked {\n type ipv4_addr\n flags timeout\n timeout 60s\n elements = { 203.0.113.1 timeout 60s expires 10000ms }\n }\n}\n")
	before, err := ManagedSemanticSHA256(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []time.Duration{3 * time.Second, 11 * time.Second} {
		actual, err := materializeManagedNFTCandidate(raw, now.UnixNano(), now.Add(elapsed))
		if err != nil {
			t.Fatal(err)
		}
		after, err := ManagedSemanticSHA256(actual)
		if err != nil || before != after {
			t.Fatalf("temporal materialization changed committed semantics: %v", err)
		}
		if elapsed == 3*time.Second && !strings.Contains(string(actual), "expires 7000ms") {
			t.Fatal("candidate renewed original lifetime")
		}
		if elapsed == 11*time.Second && strings.Contains(string(actual), "203.0.113.1") {
			t.Fatal("candidate revived expired member")
		}
	}
	for _, current := range []time.Time{now.Add(-time.Second), now.Add(61 * time.Second)} {
		if _, err := materializeManagedNFTCandidate(raw, now.UnixNano(), current); err == nil {
			t.Fatal("untrusted capture time accepted")
		}
	}
}
