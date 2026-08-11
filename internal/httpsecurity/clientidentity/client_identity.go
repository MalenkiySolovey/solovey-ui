// Package clientidentity resolves the one request-scoped client/proxy fact used
// by authentication, audit, cookies, security headers, rate limits, and WS.
package clientidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	MaxTrustedProxyCIDRs = 64
	MaxForwardedHops     = 64

	ProvenanceDirect     = "DIRECT"
	ProvenanceTrustedXFF = "TRUSTED_XFF"
	ProvenanceUnknown    = "UNKNOWN"
)

type V1 struct {
	Version          int    `json:"version"`
	TransportPeer    string `json:"transportPeer"`
	ClientIP         string `json:"clientIp"`
	ClientPrefix     string `json:"clientPrefix"`
	Provenance       string `json:"provenance"`
	TrustedProxyHops int    `json:"trustedProxyHops"`
	ActualScheme     string `json:"actualScheme"`
	DesiredScheme    string `json:"desiredScheme"`
	SchemeSource     string `json:"schemeSource"`
	ExternalHost     string `json:"externalHost"`
	ConfigRevision   string `json:"configRevision"`
	ForwardedValid   bool   `json:"forwardedValid"`
}

type Config struct {
	TrustedProxies []netip.Prefix
	CanonicalCIDRs []string
	Warnings       []string
	Source         string
	Revision       string
}

// BindingRevision returns a secret-free digest of the complete request-scoped
// client/proxy authority. Security grants can persist this digest without
// retaining a raw client address while still becoming unusable when the
// direct/proxy path, trusted-proxy configuration, forwarded chain, scheme, or
// external origin changes.
func BindingRevision(identity V1) string {
	material := strings.Join([]string{
		"client-identity-binding-v1",
		"version=" + strconv.Itoa(identity.Version),
		"transportPeer=" + CanonicalIP(identity.TransportPeer),
		"clientIP=" + CanonicalIP(identity.ClientIP),
		"clientPrefix=" + identity.ClientPrefix,
		"provenance=" + identity.Provenance,
		"trustedProxyHops=" + strconv.Itoa(identity.TrustedProxyHops),
		"actualScheme=" + identity.ActualScheme,
		"desiredScheme=" + identity.DesiredScheme,
		"schemeSource=" + identity.SchemeSource,
		"externalHost=" + CanonicalHostPort(identity.ExternalHost),
		"configRevision=" + identity.ConfigRevision,
		"forwardedValid=" + strconv.FormatBool(identity.ForwardedValid),
	}, "\n") + "\n"
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

var configCache struct {
	sync.Mutex
	initialized bool
	raw         string
	config      Config
}

func ResolveRequest(r *http.Request) V1 {
	return Resolve(r, ConfigFromEnvironment())
}

func Resolve(r *http.Request, config Config) V1 {
	peer := CanonicalIP(splitRemoteIP(r.RemoteAddr))
	actualScheme := "http"
	if r.TLS != nil {
		actualScheme = "https"
	}
	identity := V1{
		Version:        1,
		TransportPeer:  peer,
		ClientIP:       peer,
		ClientPrefix:   PrivacyPrefix(peer),
		Provenance:     ProvenanceDirect,
		ActualScheme:   actualScheme,
		DesiredScheme:  actualScheme,
		SchemeSource:   "transport",
		ExternalHost:   CanonicalHostPort(r.Host),
		ConfigRevision: config.Revision,
		ForwardedValid: true,
	}
	if !contains(config.TrustedProxies, peer) {
		return identity
	}

	// A trusted transport peer is not itself the end client. Until a bounded
	// right-to-left XFF walk finds the first untrusted hop, keep provenance
	// explicitly unknown and rate-limit on the transport peer.
	identity.Provenance = ProvenanceUnknown
	forwardedForValues := r.Header.Values("X-Forwarded-For")
	forwardedFor := strings.Join(forwardedForValues, ",")
	parts, valid := boundedForwardedFor(forwardedFor)
	if valid {
		identity.ForwardedValid = true
		trustedHops := 1 // transport peer
		for i := len(parts) - 1; i >= 0; i-- {
			hop := CanonicalIP(parts[i])
			if hop == "" {
				identity.ForwardedValid = false
				break
			}
			if contains(config.TrustedProxies, hop) {
				trustedHops++
				continue
			}
			identity.ClientIP = hop
			identity.ClientPrefix = PrivacyPrefix(hop)
			identity.TrustedProxyHops = trustedHops
			identity.Provenance = ProvenanceTrustedXFF
			break
		}
	} else if strings.TrimSpace(forwardedFor) != "" {
		identity.ForwardedValid = false
	}

	protoValues := r.Header.Values("X-Forwarded-Proto")
	if len(protoValues) == 1 {
		proto, ok := exactForwardedProto(protoValues[0])
		if !ok {
			identity.ForwardedValid = false
			return identity
		}
		identity.DesiredScheme = proto
		identity.SchemeSource = "trusted_x_forwarded_proto"
	} else if len(protoValues) > 1 {
		identity.ForwardedValid = false
	}
	return identity
}

func ConfigFromEnvironment() Config {
	raw := strings.TrimSpace(os.Getenv("SUI_TRUSTED_PROXIES"))
	configCache.Lock()
	defer configCache.Unlock()
	if configCache.initialized && raw == configCache.raw {
		return cloneConfig(configCache.config)
	}
	configCache.initialized = true
	configCache.raw = raw
	configCache.config = ParseConfig(raw)
	return cloneConfig(configCache.config)
}

func ParseConfig(raw string) Config {
	entries := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, min(len(entries), MaxTrustedProxyCIDRs))
	warnings := make([]string, 0, 3)
	seenWarnings := map[string]struct{}{}
	addWarning := func(value string) {
		if _, exists := seenWarnings[value]; exists {
			return
		}
		seenWarnings[value] = struct{}{}
		warnings = append(warnings, value)
	}
	nonemptyEntries := 0
	for _, item := range entries {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		nonemptyEntries++
		if len(prefixes) >= MaxTrustedProxyCIDRs {
			addWarning("trusted_proxy_limit_exceeded")
			continue
		}
		var prefix netip.Prefix
		if parsedPrefix, err := netip.ParsePrefix(item); err == nil {
			address := parsedPrefix.Addr()
			bits := parsedPrefix.Bits()
			if address.Is4In6() {
				if bits < 96 {
					addWarning("invalid_trusted_proxy_entry")
					continue
				}
				address = address.Unmap()
				bits -= 96
			}
			if bits > address.BitLen() {
				bits = address.BitLen()
			}
			prefix = netip.PrefixFrom(address, bits).Masked()
			prefixes = append(prefixes, prefix)
		} else if address, err := netip.ParseAddr(item); err == nil {
			address = address.Unmap()
			prefix = netip.PrefixFrom(address, address.BitLen())
			prefixes = append(prefixes, prefix)
		} else {
			addWarning("invalid_trusted_proxy_entry")
			continue
		}
		if !prefix.Addr().IsLoopback() {
			if prefix.Addr().IsPrivate() {
				if prefix.Bits() < prefix.Addr().BitLen() {
					addWarning("broad_private_trusted_proxy_cidr")
				}
			} else {
				addWarning("public_trusted_proxy")
				if prefix.Bits() < prefix.Addr().BitLen() {
					addWarning("broad_public_trusted_proxy_cidr")
				}
			}
		}
	}
	revisionInput := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		revisionInput = append(revisionInput, prefix.String())
	}
	revisionMaterial := append([]string(nil), revisionInput...)
	revisionMaterial = append(revisionMaterial, "--warnings--")
	revisionMaterial = append(revisionMaterial, warnings...)
	revisionMaterial = append(revisionMaterial, "entries="+strconv.Itoa(nonemptyEntries))
	sum := sha256.Sum256([]byte(strings.Join(revisionMaterial, ",")))
	source := "default_empty"
	if nonemptyEntries > 0 {
		source = "environment"
	}
	return Config{
		TrustedProxies: prefixes,
		CanonicalCIDRs: append([]string(nil), revisionInput...),
		Warnings:       warnings,
		Source:         source,
		Revision:       hex.EncodeToString(sum[:]),
	}
}

func CanonicalIP(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	if value == "" || strings.Contains(value, "%") {
		return ""
	}
	address, err := netip.ParseAddr(value)
	if err != nil || address.Zone() != "" {
		return ""
	}
	return address.Unmap().String()
}

func PrivacyPrefix(value string) string {
	address, err := netip.ParseAddr(CanonicalIP(value))
	if err != nil {
		return ""
	}
	bits := 24
	if address.Is6() {
		bits = 56
	}
	return netip.PrefixFrom(address, bits).Masked().String()
}

func CanonicalHostPort(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "/\\@,;?#") ||
		strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return ""
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		host = canonicalHostname(host)
		portNumber, portErr := strconv.Atoi(port)
		if host == "" || portErr != nil || portNumber < 1 || portNumber > 65535 {
			return ""
		}
		return net.JoinHostPort(host, strconv.Itoa(portNumber))
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		host := canonicalHostname(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		if address, err := netip.ParseAddr(host); err != nil || !address.Is6() {
			return ""
		}
		return "[" + host + "]"
	}
	// An IPv6 literal in an HTTP Host field must be bracketed. Rejecting an
	// unbracketed colon also removes host/port parsing ambiguity.
	if strings.Contains(value, ":") {
		return ""
	}
	return canonicalHostname(value)
}

func canonicalHostname(value string) string {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(strings.Trim(value, "[]"))), ".")
	if value == "" {
		return ""
	}
	if address, err := netip.ParseAddr(value); err == nil && address.Zone() == "" {
		return address.Unmap().String()
	}
	if len(value) > 253 {
		return ""
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return ""
			}
		}
	}
	return value
}

func exactForwardedProto(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, ",") {
		return "", false
	}
	switch strings.ToLower(value) {
	case "http":
		return "http", true
	case "https":
		return "https", true
	default:
		return "", false
	}
}

func boundedForwardedFor(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true
	}
	parts := strings.Split(value, ",")
	if len(parts) > MaxForwardedHops {
		return nil, false
	}
	return parts, true
}

func contains(prefixes []netip.Prefix, value string) bool {
	address, err := netip.ParseAddr(CanonicalIP(value))
	if err != nil {
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func splitRemoteIP(value string) string {
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

func cloneConfig(config Config) Config {
	return Config{
		TrustedProxies: append([]netip.Prefix(nil), config.TrustedProxies...),
		CanonicalCIDRs: append([]string(nil), config.CanonicalCIDRs...),
		Warnings:       append([]string(nil), config.Warnings...),
		Source:         config.Source,
		Revision:       config.Revision,
	}
}
