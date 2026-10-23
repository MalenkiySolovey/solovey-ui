package jsonvalue

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictObjectRejectsAmbiguousOrUnboundedInputWithoutRewriting(t *testing.T) {
	for _, raw := range []string{
		`{}`, ` {"number":9007199254740993,"nested":[null,true,{"a":"b"}]} `,
		`{"a":1,"nested":{"a":2}}`,
	} {
		input := []byte(raw)
		before := bytes.Clone(input)
		if err := StrictObject(input); err != nil || !bytes.Equal(before, input) {
			t.Fatalf("valid object changed/rejected: %v", err)
		}
	}
	for _, raw := range []string{
		``, `[]`, `null`, `true`, `{`, `{"a":}`, `{"a":1,}`, `{"a":1} {}`,
		`{"a":1} trailing`, `{"a":1,"a":2}`, `{"a":1,"\u0061":2}`,
		`{"array":[{"credential-sentinel":1,"credential-sentinel":2}]}`,
		"{\"a\":\"\xff\"}",
	} {
		if err := StrictObject([]byte(raw)); err == nil || strings.Contains(err.Error(), "credential-sentinel") {
			t.Fatalf("invalid object accepted or content echoed: %v", err)
		}
	}
	if err := StrictObject([]byte(`{"a":` + strings.Repeat(`[`, MaxStrictDepth) + `0` + strings.Repeat(`]`, MaxStrictDepth) + `}`)); err == nil {
		t.Fatal("JSON depth bound ignored")
	}
	if err := StrictObject([]byte(`{"a":` + strings.Repeat(`[`, MaxStrictDepth-1) + `0` + strings.Repeat(`]`, MaxStrictDepth-1) + `}`)); err != nil {
		t.Fatalf("inclusive JSON depth rejected: %v", err)
	}
	// Exercise the node bound directly without allocating a megabyte fixture.
	decoder := json.NewDecoder(strings.NewReader(`0`))
	members := MaxStrictMembers
	if err := strictValue(decoder, 0, &members); err == nil {
		t.Fatal("JSON member bound ignored")
	}
}
