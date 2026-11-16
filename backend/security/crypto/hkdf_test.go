package crypto

import (
	"bytes"
	"testing"
)

func TestDeriveHKDFKeyIsDomainSeparated(t *testing.T) {
	master := []byte("test-master-key-material-32-bytes!!")
	cookie, err := DeriveHKDFKey(master, nil, []byte("sui:cookie:v1"))
	if err != nil {
		t.Fatalf("derive cookie: %v", err)
	}
	settings, err := DeriveHKDFKey(master, nil, []byte("sui:settings-secretbox:v1"))
	if err != nil {
		t.Fatalf("derive settings: %v", err)
	}
	if len(cookie) != 32 || len(settings) != 32 {
		t.Fatalf("unexpected key lengths: %d, %d", len(cookie), len(settings))
	}
	if bytes.Equal(cookie, settings) {
		t.Fatal("domain-separated keys should differ")
	}
}

func TestDeriveHKDFKeyRejectsEmptyMaster(t *testing.T) {
	if _, err := DeriveHKDFKey(nil, nil, []byte("test")); err == nil || err.Error() != "empty master key" {
		t.Fatalf("empty master key error = %v", err)
	}
}
