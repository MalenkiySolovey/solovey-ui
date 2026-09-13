package policy

import (
	"errors"
	"net/netip"
	"strings"
)

var (
	ErrTrustedSourceInvalid           = errors.New("trusted source must be an IP address or CIDR prefix")
	ErrTrustedSourceUnusable          = errors.New("trusted source is not usable for remote management")
	ErrTrustedSourceBroadConfirmation = errors.New("broad trusted source requires exact confirmation")
)

type TrustedSourceValidation struct {
	Prefix                    netip.Prefix
	Family                    string
	Broad                     bool
	Confirmation              string
	BroadScopeAcknowledgement bool
}

// ValidateTrustedSource is the single semantic admission contract for a
// permanent administrator source. It normalizes host input, rejects ranges
// containing special-purpose local/non-unicast addresses, and makes broad
// grants require an exact typed confirmation.
func ValidateTrustedSource(raw string, broadAcknowledged bool, confirmation string) (TrustedSourceValidation, error) {
	prefix, err := normalizeTrustedSource(raw)
	if err != nil {
		return TrustedSourceValidation{}, err
	}
	if trustedSourceContainsUnusableAddress(prefix) {
		return TrustedSourceValidation{}, ErrTrustedSourceUnusable
	}
	family, broad := "ipv6", prefix.Bits() < 64
	if prefix.Addr().Is4() {
		family, broad = "ipv4", prefix.Bits() < 24
	}
	expected := "TRUST BROAD SOURCE " + prefix.String()
	if broad && (!broadAcknowledged || confirmation != expected) {
		return TrustedSourceValidation{Prefix: prefix, Family: family, Broad: true, Confirmation: expected}, ErrTrustedSourceBroadConfirmation
	}
	return TrustedSourceValidation{Prefix: prefix, Family: family, Broad: broad, Confirmation: expected, BroadScopeAcknowledgement: broadAcknowledged}, nil
}

// NormalizeTrustedSource validates semantic address usability without
// asserting how an already-admitted broad source was acknowledged. It is used
// only after the persistence boundary has checked that acknowledgement.
func NormalizeTrustedSource(raw string) (netip.Prefix, error) {
	value, err := ValidateTrustedSource(raw, true, "")
	if errors.Is(err, ErrTrustedSourceBroadConfirmation) {
		return value.Prefix, nil
	}
	return value.Prefix, err
}

func normalizeTrustedSource(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return netip.Prefix{}, ErrTrustedSourceInvalid
			}
			return netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96).Masked(), nil
		}
		return prefix.Masked(), nil
	}
	address, err := netip.ParseAddr(raw)
	if err != nil || address.Zone() != "" {
		return netip.Prefix{}, ErrTrustedSourceInvalid
	}
	address = address.Unmap()
	bits := 128
	if address.Is4() {
		bits = 32
	}
	return netip.PrefixFrom(address, bits), nil
}

func trustedSourceContainsUnusableAddress(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return true
	}
	address := prefix.Addr()
	if address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return true
	}
	var special []netip.Addr
	if prefix.Addr().Is4() {
		special = []netip.Addr{
			netip.MustParseAddr("0.0.0.0"), netip.MustParseAddr("127.0.0.1"),
			netip.MustParseAddr("169.254.0.1"), netip.MustParseAddr("224.0.0.1"),
			netip.MustParseAddr("255.255.255.255"),
		}
	} else {
		special = []netip.Addr{
			netip.IPv6Unspecified(), netip.IPv6Loopback(),
			netip.MustParseAddr("fe80::1"), netip.MustParseAddr("ff00::1"),
		}
	}
	for _, address := range special {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
