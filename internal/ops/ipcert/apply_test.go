package ipcert

import (
	"encoding/json"
	"testing"
)

func TestPatchTLSServerBlock(t *testing.T) {
	out, err := PatchTLSServerBlock(nil, "/c/cert.crt", "/c/key.key")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["certificate_path"] != "/c/cert.crt" || m["key_path"] != "/c/key.key" {
		t.Fatalf("paths not set on empty block: %v", m)
	}

	existing := []byte(`{"server_name":"x","certificate":["AAA"],"key":["BBB"],"min_version":"1.3"}`)
	out, err = PatchTLSServerBlock(existing, "/c2.crt", "/k2.key")
	if err != nil {
		t.Fatal(err)
	}
	m = map[string]any{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["certificate"]; ok {
		t.Error("inline certificate not removed")
	}
	if _, ok := m["key"]; ok {
		t.Error("inline key not removed")
	}
	if m["server_name"] != "x" || m["min_version"] != "1.3" {
		t.Errorf("unrelated fields lost: %v", m)
	}
	if m["certificate_path"] != "/c2.crt" || m["key_path"] != "/k2.key" {
		t.Errorf("paths not set on existing block: %v", m)
	}

	if _, err := PatchTLSServerBlock([]byte(`["not","an","object"]`), "c", "k"); err == nil {
		t.Fatal("PatchTLSServerBlock on non-object = nil, want error")
	}
}
