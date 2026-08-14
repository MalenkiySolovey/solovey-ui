//go:build linux

package main

import (
	"bytes"
	"testing"
)

func TestNewInstanceIDIsCanonicalUUIDv4(t *testing.T) {
	value, err := newInstanceID(bytes.NewReader([]byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if value != "00112233-4455-4677-8899-aabbccddeeff" || !validUUID(value) {
		t.Fatalf("unexpected instance identity %q", value)
	}
}
