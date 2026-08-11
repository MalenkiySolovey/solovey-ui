package envelope

import (
	"bytes"
	"testing"
)

func TestStreamEnvelopeRoundTripTamperTruncationAndBounds(t *testing.T) {
	plain := bytes.Repeat([]byte("streamed-backup-block"), 170000)
	passphrase := []byte("correct horse battery staple")
	var encrypted bytes.Buffer
	plainBytes, cipherBytes, err := SealStream(&encrypted, bytes.NewReader(plain), passphrase)
	if err != nil || plainBytes != int64(len(plain)) || cipherBytes != int64(encrypted.Len()) {
		t.Fatalf("seal plain=%d cipher=%d len=%d err=%v", plainBytes, cipherBytes, encrypted.Len(), err)
	}
	if header, err := ParseHeader(encrypted.Bytes()); err != nil || header.Version != VersionStream {
		t.Fatalf("stream header=%#v err=%v", header, err)
	}
	var opened bytes.Buffer
	if _, _, err := OpenStream(&opened, bytes.NewReader(encrypted.Bytes()), passphrase, MaxStreamBytes); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened.Bytes(), plain) {
		t.Fatal("stream envelope round trip changed plaintext")
	}
	for _, mutation := range []func([]byte) []byte{
		func(value []byte) []byte { value[len(value)/2] ^= 1; return value },
		func(value []byte) []byte { return value[:len(value)-1] },
		func(value []byte) []byte { return append(value, 1) },
	} {
		candidate := mutation(append([]byte(nil), encrypted.Bytes()...))
		if _, _, err := OpenStream(&bytes.Buffer{}, bytes.NewReader(candidate), passphrase, MaxStreamBytes); err == nil {
			t.Fatal("invalid streamed envelope was accepted")
		}
	}
	if _, _, err := OpenStream(&bytes.Buffer{}, bytes.NewReader(encrypted.Bytes()), passphrase, int64(len(plain)-1)); err == nil {
		t.Fatal("plaintext limit was not enforced")
	}
}
