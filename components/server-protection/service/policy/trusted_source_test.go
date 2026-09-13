package policy

import (
	"errors"
	"testing"
)

func TestTrustedSourceNormalizationAndFamily(t *testing.T) {
	tests := []struct {
		input, canonical, family string
	}{
		{"198.51.100.7", "198.51.100.7/32", "ipv4"},
		{"198.51.100.7/24", "198.51.100.0/24", "ipv4"},
		{"2001:db8::7", "2001:db8::7/128", "ipv6"},
		{"2001:db8::7/64", "2001:db8::/64", "ipv6"},
		{"::ffff:198.51.100.7/128", "198.51.100.7/32", "ipv4"},
	}
	for _, test := range tests {
		got, err := ValidateTrustedSource(test.input, false, "")
		if err != nil || got.Prefix.String() != test.canonical || got.Family != test.family || got.Broad {
			t.Fatalf("input=%q got=%#v err=%v", test.input, got, err)
		}
	}
}

func TestTrustedSourceRejectsUnusableAndAmbiguousAddresses(t *testing.T) {
	for _, input := range []string{"", "not-an-address", "0.0.0.0", "0.0.0.0/0", "127.0.0.1", "169.254.1.2", "224.0.0.1", "255.255.255.255", "::", "::/0", "::1", "fe80::1", "ff02::1", "::ffff:192.0.2.1/80"} {
		if _, err := ValidateTrustedSource(input, true, "TRUST BROAD SOURCE "+input); err == nil {
			t.Fatalf("unusable trusted source accepted: %q", input)
		}
	}
}

func TestBroadTrustedSourceRequiresExactTypedConfirmation(t *testing.T) {
	const source = "10.0.0.0/8"
	got, err := ValidateTrustedSource(source, false, "")
	if !errors.Is(err, ErrTrustedSourceBroadConfirmation) || !got.Broad || got.Confirmation != "TRUST BROAD SOURCE "+source {
		t.Fatalf("unconfirmed broad source=%#v err=%v", got, err)
	}
	if _, err := ValidateTrustedSource(source, true, "trust broad source "+source); !errors.Is(err, ErrTrustedSourceBroadConfirmation) {
		t.Fatalf("inexact confirmation accepted: %v", err)
	}
	got, err = ValidateTrustedSource(source, true, "TRUST BROAD SOURCE "+source)
	if err != nil || !got.Broad || !got.BroadScopeAcknowledgement {
		t.Fatalf("exact confirmation rejected: %#v err=%v", got, err)
	}
}
