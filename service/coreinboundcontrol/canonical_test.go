package coreinboundcontrol

import (
	"strings"
	"testing"
)

func TestCanonicalJSONIgnoresMapOrderWhitespaceAndNumberSpelling(t *testing.T) {
	left, err := canonicalDigest([]byte(`{"b":1.0,"a":{"z":1e2,"x":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalDigest([]byte(" { \"a\" : { \"x\" : true, \"z\" : 100 }, \"b\" : 1 } "))
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("canonical digests differ: %s != %s", left, right)
	}
}

func TestCanonicalJSONRejectsDuplicateKeysAndTrailingValues(t *testing.T) {
	for _, content := range []string{`{"a":1,"a":2}`, `{"a":1} {"b":2}`, `{"a":1e99999}`} {
		if _, err := canonicalJSON([]byte(content)); err == nil {
			t.Fatalf("accepted malformed JSON: %s", content)
		}
	}
}

func TestCanonicalJSONRejectsOutputExpansionPastBound(t *testing.T) {
	content := "[" + strings.Repeat("1e4096,", 256) + "1e4096]"
	if len(content) >= maxCanonicalJSONSize {
		t.Fatalf("test input is not bounded: %d", len(content))
	}
	if _, err := canonicalJSON([]byte(content)); err == nil {
		t.Fatal("accepted canonical output larger than the configured bound")
	}
}
