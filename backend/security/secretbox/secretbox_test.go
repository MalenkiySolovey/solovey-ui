package secretbox

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box, err := NewFromString("test-master-key")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := box.EncryptString("fixture-token", "fixtureBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "fixture-token" || !IsEncrypted(encrypted) {
		t.Fatalf("value was not encrypted: %q", encrypted)
	}
	decrypted, err := box.DecryptString(encrypted, "fixtureBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "fixture-token" {
		t.Fatalf("unexpected plaintext %q", decrypted)
	}
}

func TestDecryptRejectsWrongAssociatedData(t *testing.T) {
	box, err := NewFromString("test-master-key")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := box.EncryptString("fixture-token", "fixtureBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.DecryptString(encrypted, "fixtureProxyURL"); err == nil {
		t.Fatal("expected decrypt to fail with wrong associated data")
	}
}

func TestEncryptDecryptBytesRoundTrip(t *testing.T) {
	box, err := NewFromString("test-master-key")
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte{0, 1, 2, 3, 255}
	encrypted, err := box.EncryptBytes(plain, "fixtureBackupPassphrase")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == string(plain) || !IsEncrypted(encrypted) {
		t.Fatalf("value was not encrypted: %q", encrypted)
	}
	decrypted, err := box.DecryptBytes(encrypted, "fixtureBackupPassphrase")
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != string(plain) {
		t.Fatalf("unexpected plaintext %v", decrypted)
	}
}

func TestNewRawKeyUsesProvidedAESKey(t *testing.T) {
	rawKey := []byte("0123456789abcdef0123456789abcdef")
	box, err := NewRawKey(rawKey)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := box.EncryptString("fixture-token", "fixtureBotToken")
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := box.DecryptString(encrypted, "fixtureBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "fixture-token" {
		t.Fatalf("unexpected plaintext %q", decrypted)
	}

	legacyBox, err := New(rawKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyBox.DecryptString(encrypted, "fixtureBotToken"); err == nil {
		t.Fatal("raw-key ciphertext should not decrypt with legacy HKDF constructor")
	}
}
