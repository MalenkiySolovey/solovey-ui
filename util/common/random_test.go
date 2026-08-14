package common

import "testing"

func TestSecureRandomReturnsRequestedAlphabeticLength(t *testing.T) {
	value, err := SecureRandom(64)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 64 {
		t.Fatalf("secure random length = %d, want 64", len(value))
	}
	for _, character := range value {
		found := false
		for _, allowed := range allSeq {
			if character == allowed {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("secure random contains unexpected character %q", character)
		}
	}
	if _, err := SecureRandom(0); err == nil {
		t.Fatal("zero-length secure random request was accepted")
	}
}
