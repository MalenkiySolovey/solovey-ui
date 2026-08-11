package handoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

func (s *Service) preflight(ctx context.Context, plan Plan) error {
	if strings.TrimSpace(plan.PlanRevision) == "" || strings.TrimSpace(plan.IdempotencyKey) == "" || strings.TrimSpace(plan.Actor) == "" {
		return ErrInvalidPlan
	}
	if err := validateSnapshot(plan.Previous); err != nil {
		return err
	}
	if err := validateSnapshot(plan.Next); err != nil {
		return err
	}
	if plan.Previous.Protocol != plan.Next.Protocol || canonicalListen(plan.Previous.Listen) != canonicalListen(plan.Next.Listen) || plan.Previous.Port != plan.Next.Port {
		return fmt.Errorf("%w: handoff must preserve exact socket identity", ErrInvalidPlan)
	}
	switch strings.ToLower(strings.TrimSpace(plan.Previous.Kind)) {
	case "panel", "panel_web", "admin", "subscription", "ssh":
		return ErrCriticalOwner
	}
	previousKind := strings.ToLower(strings.TrimSpace(plan.Previous.Kind))
	if previousKind != "public_site" && previousKind != "fallback" && previousKind != "fallback_site" {
		return fmt.Errorf("%w: handoff source must be a fallback/public site", ErrInvalidPlan)
	}
	if !strings.EqualFold(strings.TrimSpace(plan.Next.Kind), "inbound") || !strings.EqualFold(strings.TrimSpace(plan.Next.Owner), "core") || plan.Previous.ResourceID == plan.Next.ResourceID {
		return fmt.Errorf("%w: handoff target must be a distinct core-owned sing-box inbound", ErrInvalidPlan)
	}
	if wildcard(plan.Previous.Listen) && (!plan.AdvancedConfirmed || plan.AdvancedPhrase != "ALLOW WILDCARD LISTENER "+plan.PlanRevision) {
		return ErrWildcardConfirm
	}
	if acmeConflict(plan.Previous, plan.Next) {
		return ErrACMEConflict
	}
	if err := validateProfile(plan.Next); err != nil {
		return err
	}
	caps, err := s.Helper.Capabilities(ctx)
	if err != nil {
		return err
	}
	if caps.Revision == "" || !caps.InboundDraft || !caps.SingBoxRestart || !caps.ListenerOwnership || !caps.FallbackTarget || !caps.Health {
		return ErrServiceDisabled
	}
	if !caps.ExactListener || wildcard(plan.Previous.Listen) {
		return ErrExactListener
	}
	if plan.Previous.ProxyProtocol != plan.Next.ProxyProtocol {
		return ErrProxyCapability
	}
	if plan.Next.ProxyProtocol && !caps.ProxyProtocol {
		return ErrProxyCapability
	}
	manifest, err := s.Ownership.Manifest(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range manifest {
		if candidate.ResourceID != plan.Previous.ResourceID && overlaps(candidate, plan.Previous) {
			return fmt.Errorf("%w: %s", ErrCollision, candidate.ResourceID)
		}
	}
	return nil
}

func (s *Service) revalidate(ctx context.Context, previous, next OwnerSnapshot) error {
	current, err := s.Ownership.Current(ctx, previous.Protocol, previous.Listen, previous.Port)
	if err != nil {
		return ErrOwnerDisappeared
	}
	if current.ResourceID != previous.ResourceID {
		return ErrOwnerDisappeared
	}
	if current.Owner != previous.Owner || current.Kind != previous.Kind || !strings.EqualFold(current.Protocol, previous.Protocol) || canonicalListen(current.Listen) != canonicalListen(previous.Listen) || current.Port != previous.Port || current.ProxyProtocol != previous.ProxyProtocol {
		return ErrRevisionConflict
	}
	if current.ResourceRevision != previous.ResourceRevision || current.ConfigRevision != previous.ConfigRevision || current.Fingerprint != previous.Fingerprint {
		return ErrRevisionConflict
	}
	manifest, err := s.Ownership.Manifest(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range manifest {
		if candidate.ResourceID != previous.ResourceID && overlaps(candidate, next) {
			return fmt.Errorf("%w: %s", ErrCollision, candidate.ResourceID)
		}
	}
	return nil
}

func validateSnapshot(v OwnerSnapshot) error {
	if strings.TrimSpace(v.ResourceID) == "" || strings.TrimSpace(v.Owner) == "" || v.Port < 1 || v.Port > 65535 || strings.TrimSpace(v.ResourceRevision) == "" || strings.TrimSpace(v.ConfigRevision) == "" || strings.TrimSpace(v.Fingerprint) == "" {
		return ErrInvalidPlan
	}
	if strings.ToLower(v.Protocol) != "tcp" {
		return ErrProtocol
	}
	if !wildcard(v.Listen) {
		if _, err := netip.ParseAddr(strings.Trim(v.Listen, "[]")); err != nil {
			return fmt.Errorf("%w: listen must be an IP literal", ErrInvalidPlan)
		}
	}
	return nil
}
func validateProfile(v OwnerSnapshot) error {
	p := v.Profile
	switch strings.ToLower(p.Protocol) {
	case "vless":
		if strings.ToLower(p.Security) != "reality" || !p.StrictSNI || strings.TrimSpace(p.HandshakeHost) == "" || !loopback(p.FallbackListen) || p.FallbackPort < 1 || p.FallbackPort > 65535 {
			return fmt.Errorf("%w: VLESS/REALITY requires strict SNI and a loopback fallback", ErrInvalidPlan)
		}
	case "trojan":
		if strings.ToLower(p.Security) != "tls" || strings.TrimSpace(p.CertificateRef) == "" || !loopback(p.FallbackListen) || p.FallbackPort < 1 || len(p.ALPNFallbacks) == 0 {
			return fmt.Errorf("%w: Trojan requires certificate reference, loopback fallback and ALPN fallback", ErrInvalidPlan)
		}
		for alpn, target := range p.ALPNFallbacks {
			if (alpn != "h2" && alpn != "http/1.1") || strings.TrimSpace(target) == "" {
				return fmt.Errorf("%w: unsupported Trojan ALPN fallback", ErrInvalidPlan)
			}
		}
	default:
		return ErrProtocol
	}
	return nil
}
func loopback(value string) bool {
	parsed, err := netip.ParseAddr(strings.Trim(value, "[]"))
	return err == nil && parsed.IsLoopback()
}
func wildcard(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), "[]")
	return value == "0.0.0.0" || value == "::"
}
func acmeConflict(a, b OwnerSnapshot) bool {
	if !a.ACMERenewal && !b.ACMERenewal {
		return false
	}
	for _, route := range append(append([]string(nil), a.ReservedRoutes...), b.ReservedRoutes...) {
		if strings.HasPrefix(route, "/.well-known/acme-challenge") {
			return true
		}
	}
	return false
}
func overlaps(a, b OwnerSnapshot) bool {
	if !strings.EqualFold(a.Protocol, b.Protocol) || a.Port != b.Port {
		return false
	}
	return canonicalListen(a.Listen) == canonicalListen(b.Listen) || wildcard(a.Listen) || wildcard(b.Listen)
}

func canonicalListen(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), "[]")
	address, err := netip.ParseAddr(trimmed)
	if err != nil {
		return trimmed
	}
	return address.Unmap().String()
}

// Fingerprint returns a deterministic presentation-safe manifest digest for
// callers that assemble typed snapshots from resource contributors.
func Fingerprint(v OwnerSnapshot) string {
	clone := cloneSnapshot(v)
	clone.Fingerprint = ""
	raw, _ := json.Marshal(clone)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
