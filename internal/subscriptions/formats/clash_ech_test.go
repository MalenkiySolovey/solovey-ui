package formats

import (
	"bytes"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

func TestClashECHPreservesAllListEntries(t *testing.T) {
	first := []byte{0, 5, 0xfe, 0x0d, 0, 1, 1}
	second := []byte{0, 6, 0xfe, 0x0e, 0, 2, 2, 3}
	block := func(b []byte) string { return string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: b})) }
	for _, value := range []any{base64.StdEncoding.EncodeToString(first), block(first), strings.Split(block(first), "\n")} {
		text, err := clashECHConfig(value)
		if err != nil {
			t.Fatal(err)
		}
		list, err := base64.StdEncoding.DecodeString(text)
		if err != nil || !bytes.Equal(list, first) {
			t.Fatal("single list entries changed")
		}
	}
	text, err := clashECHConfig([]string{block(first), block(second)})
	if err != nil {
		t.Fatal(err)
	}
	list, err := base64.StdEncoding.DecodeString(text)
	want := append([]byte{0, 11}, first[2:]...)
	want = append(want, second[2:]...)
	if err != nil || !bytes.Equal(list, want) {
		t.Fatal("multiple PEM lists lost entries or retained duplicate vector prefixes")
	}
	for _, value := range []any{"not-base64", base64.StdEncoding.EncodeToString([]byte{0, 5, 1}), base64.StdEncoding.EncodeToString([]byte{0, 5, 0xfe, 0x0d, 0, 9, 1}), "junk\n" + block(first), "-----BEGIN PRIVATE KEY-----\nAA==\n-----END PRIVATE KEY-----", []any{block(first), false}} {
		if _, err := clashECHConfig(value); err == nil {
			t.Fatal("malformed/private ECH accepted")
		}
	}
}
