package envelope

import (
	"bytes"
	"errors"
	"testing"
)

func TestTelegramBackupEnvelopeExtraRoundTrip(t *testing.T) {
	random := bytes.Repeat([]byte{0x7a}, saltSize+nonceSize)
	envelope, err := build([]byte("phase2 sqlite payload"), []byte("correct horse battery staple"), bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := Open(envelope, []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "phase2 sqlite payload" {
		t.Fatalf("unexpected plaintext %q", string(plaintext))
	}
}

func TestTelegramBackupEnvelopeRejectsInvalidMagic(t *testing.T) {
	envelope := make([]byte, headerSize+16)
	copy(envelope, []byte("NOT-TGBKP\x00"))
	if _, err := Open(envelope, []byte("correct horse battery staple")); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected invalid envelope for bad magic, got %v", err)
	}
}

func TestTelegramBackupEnvelopeRejectsWrongKDFIDExtra(t *testing.T) {
	envelope, err := Build([]byte("sqlite"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	envelope[magicSize+versionSize] = 0xff
	if _, err := Open(envelope, []byte("correct horse battery staple")); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected invalid envelope for wrong KDF id, got %v", err)
	}
}

func TestTelegramBackupEnvelopeRejectsUnreadableCiphertext(t *testing.T) {
	envelope, err := Build([]byte("sqlite"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	truncated := append([]byte(nil), envelope[:len(envelope)-1]...)
	if _, err := Open(truncated, []byte("correct horse battery staple")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected decryption failure for truncated ciphertext, got %v", err)
	}
}
