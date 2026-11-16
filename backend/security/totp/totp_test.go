package totp

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

func TestRFC6238SHA1Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, test := range []struct {
		unix int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		if got := Code(secret, uint64(test.unix/30), 8); got != test.code {
			t.Fatalf("Code(%d) = %q, want %q", test.unix, got, test.code)
		}
	}
}

func TestVerifyWindowAndReplay(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(1234567890, 0)
	counter := now.Unix()/30 - 1
	code := Code([]byte("12345678901234567890"), uint64(counter), Digits)
	accepted, err := Verify(secret, code, now, -1)
	if err != nil || accepted != counter {
		t.Fatalf("Verify() = %d, %v", accepted, err)
	}
	if _, err := Verify(secret, code, now, accepted); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestProvisioningURIContainsNoUnexpectedFields(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	value, err := ProvisioningURI(secret, "admin@example", "Solovey UI")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" ||
		parsed.Query().Get("algorithm") != "SHA1" ||
		parsed.Query().Get("digits") != "6" ||
		parsed.Query().Get("period") != "30" {
		t.Fatalf("unexpected provisioning URI: %q", value)
	}
}

func BenchmarkVerifyCurrentProfile(b *testing.B) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(1_800_000_000, 0)
	raw, err := DecodeSecret(secret)
	if err != nil {
		b.Fatal(err)
	}
	counter := now.Unix() / int64(Period/time.Second)
	code := Code(raw, uint64(counter), Digits)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		accepted, verifyErr := Verify(secret, code, now, counter-1)
		if verifyErr != nil || accepted != counter {
			b.Fatalf("Verify() = %d, %v", accepted, verifyErr)
		}
	}
}
