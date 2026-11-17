package secretbox

import "testing"

// This frozen legacy envelope uses an independent HKDF/AES-GCM fixture with a
// fixed nonce. It protects persisted ciphertext across key-derivation ownership.
func TestDecryptFrozenLegacySettingsEnvelope(t *testing.T) {
	const envelope = "sbox:v1:AAECAwQFBgcICQoLhN6760PQJPRt048DYcvQMGGfqmyCCk5UnwBtUwBFdquN_4yupnYDaOxArGf64kLaV5Xnvg"
	box, err := New([]byte("historical-setting-master-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := box.DecryptString(envelope, "fixtureBotToken")
	if err != nil || plaintext != "historical persisted setting fixture" {
		t.Fatalf("persisted legacy envelope changed: %v", err)
	}
	if _, err := box.DecryptString(envelope, "different-owner"); err == nil {
		t.Fatal("legacy envelope lost associated-data binding")
	}
}
