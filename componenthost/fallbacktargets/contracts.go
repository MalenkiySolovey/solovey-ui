// Package fallbacktargets owns neutral provider facts and exact reference
// leases. It cannot publish content, select filesystem paths, or mutate traffic.
package fallbacktargets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	TargetSchemaV1 = "solovey-ui/fallback-target/v1"
	LeaseSchemaV1  = "solovey-ui/fallback-target-lease/v1"
	maxTargets     = 4096
	// MaxReferenceLeases bounds neutral and persistent lease stores. Capacity
	// exhaustion fails closed instead of growing an unbounded decision map.
	MaxReferenceLeases = 4096
	providerTimeout    = 5 * time.Second
	maxProvidersV1     = 128
)

type Readiness string

const (
	ReadinessReady    Readiness = "READY"
	ReadinessNotReady Readiness = "NOT_READY"
	ReadinessStale    Readiness = "STALE"
	ReadinessUnknown  Readiness = "UNKNOWN"
)

type TargetIdentity struct {
	ProviderID string `json:"providerId"`
	TargetID   string `json:"targetId"`
}

type EndpointCapability struct {
	EndpointID         string                        `json:"endpointId"`
	Network            hostresources.Network         `json:"network"`
	Family             hostresources.AddressFamily   `json:"family"`
	Bind               string                        `json:"bind"`
	Port               uint16                        `json:"port"`
	TLS                hostresources.CapabilityValue `json:"tls"`
	Local              bool                          `json:"local"`
	CanReachManagement hostresources.CapabilityValue `json:"canReachManagement"`
}

type TargetV1 struct {
	Schema                 string             `json:"schema"`
	Identity               TargetIdentity     `json:"identity"`
	PublishRevision        string             `json:"publishRevision"`
	ContentDigest          string             `json:"contentDigest"`
	Endpoint               EndpointCapability `json:"endpoint"`
	Readiness              Readiness          `json:"readiness"`
	ProviderHealthRevision string             `json:"providerHealthRevision"`
	ObservedAt             int64              `json:"observedAt"`
	ExpiresAt              int64              `json:"expiresAt"`
	Source                 string             `json:"source"`
	ConfidenceBP           int                `json:"confidenceBp"`
	ReasonCodes            []string           `json:"reasonCodes,omitempty"`
}

type TargetReferenceV1 struct {
	ProviderID              string `json:"providerId"`
	TargetID                string `json:"targetId"`
	PublishRevision         string `json:"publishRevision"`
	ContentDigest           string `json:"contentDigest"`
	ApprovedLocalEndpointID string `json:"approvedLocalEndpointId"`
	ProviderHealthRevision  string `json:"providerHealthRevision"`
}

type ReferenceLeaseV1 struct {
	Schema                  string   `json:"schema"`
	LeaseID                 string   `json:"leaseId"`
	HolderID                string   `json:"holderId"`
	ProviderID              string   `json:"providerId"`
	TargetID                string   `json:"targetId"`
	PublishRevision         string   `json:"publishRevision"`
	ContentDigest           string   `json:"contentDigest"`
	ApprovedLocalEndpointID string   `json:"approvedLocalEndpointId"`
	ProviderHealthRevision  string   `json:"providerHealthRevision"`
	IssuedAt                int64    `json:"issuedAt"`
	RenewedAt               int64    `json:"renewedAt"`
	ExpiresAt               int64    `json:"expiresAt"`
	ReleasedAt              int64    `json:"releasedAt,omitempty"`
	State                   string   `json:"state"`
	ReasonCodes             []string `json:"reasonCodes,omitempty"`
}

func (l ReferenceLeaseV1) Fresh(now time.Time) bool {
	return l.Validate(now) == nil && l.State == "ACTIVE" && l.ExpiresAt > now.UTC().Unix() && l.ReleasedAt == 0
}

func (l ReferenceLeaseV1) Validate(_ time.Time) error {
	if l.Schema != LeaseSchemaV1 || !validOpaqueID(l.LeaseID, 128) || !validOpaqueID(l.HolderID, 128) || l.IssuedAt <= 0 || l.RenewedAt < l.IssuedAt || l.ExpiresAt < l.RenewedAt || !validReasons(l.ReasonCodes) {
		return errors.New("fallback lease contract is invalid")
	}
	if err := validReference(TargetReferenceV1{l.ProviderID, l.TargetID, l.PublishRevision, l.ContentDigest, l.ApprovedLocalEndpointID, l.ProviderHealthRevision}); err != nil {
		return err
	}
	switch l.State {
	case "ACTIVE":
		if l.ReleasedAt != 0 {
			return errors.New("active fallback lease has a release time")
		}
	case "STALE":
	case "RELEASED":
		if l.ReleasedAt <= 0 || l.ExpiresAt > l.ReleasedAt {
			return errors.New("released fallback lease time is invalid")
		}
	default:
		return errors.New("fallback lease state is invalid")
	}
	return nil
}

type Provider interface {
	ProviderID() string
	ListTargets(context.Context) ([]TargetV1, error)
}

type Snapshot struct {
	GeneratedAt int64      `json:"generatedAt"`
	Targets     []TargetV1 `json:"targets"`
	ReasonCodes []string   `json:"reasonCodes,omitempty"`
}

type Registry struct {
	mu          sync.RWMutex
	next        uint64
	providers   map[uint64]Provider
	v2Providers map[uint64]ProviderV2
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[uint64]Provider), v2Providers: make(map[uint64]ProviderV2)}
}

func (r *Registry) Register(provider Provider) func() {
	if provider == nil {
		return func() {}
	}
	r.mu.Lock()
	if len(r.providers) >= maxProvidersV1 {
		r.mu.Unlock()
		panic("fallback target provider v1 registry capacity exceeded")
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	r.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }
}

func (r *Registry) Snapshot(ctx context.Context, now time.Time) Snapshot {
	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderID() < providers[j].ProviderID() })
	result := Snapshot{GeneratedAt: now.UTC().Unix(), Targets: []TargetV1{}}
	if len(providers) == 0 {
		result.ReasonCodes = []string{"fallback_target_provider_absent"}
		return result
	}
	seen := map[string]struct{}{}
	for _, provider := range providers {
		if len(result.Targets) >= maxTargets {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_inventory_truncated")
			break
		}
		providerID := strings.TrimSpace(provider.ProviderID())
		if !validOpaqueID(providerID, 128) {
			providerID = opaqueID("provider", providerID)
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_identity_invalid")
		}
		providerCtx, cancel := context.WithTimeout(ctx, providerTimeout)
		items, err := provider.ListTargets(providerCtx)
		cancel()
		if err != nil {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_unavailable")
			continue
		}
		remaining := maxTargets - len(result.Targets)
		if len(items) > remaining {
			items = items[:remaining]
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_inventory_truncated")
		}
		for _, target := range items {
			target.Schema = TargetSchemaV1
			target = sanitizeTarget(target)
			if target.Identity.ProviderID == "" {
				target.Identity.ProviderID = providerID
			} else if target.Identity.ProviderID != providerID {
				target.Readiness = ReadinessUnknown
				target.ReasonCodes = append(target.ReasonCodes, "fallback_target_provider_identity_mismatch")
			}
			key := target.Identity.ProviderID + "\x00" + target.Identity.TargetID
			if _, ok := seen[key]; ok {
				result.ReasonCodes = append(result.ReasonCodes, "duplicate_fallback_target_id")
				continue
			}
			seen[key] = struct{}{}
			target.ReasonCodes = normalizeReasons(target.ReasonCodes)
			if err := validateTarget(target, now); err != nil {
				target.Readiness = ReadinessUnknown
				target.ReasonCodes = normalizeReasons(append(target.ReasonCodes, "fallback_target_invalid"))
			}
			if target.ExpiresAt > 0 && target.ExpiresAt <= now.UTC().Unix() {
				target.Readiness = ReadinessStale
				target.ReasonCodes = normalizeReasons(append(target.ReasonCodes, "fallback_target_stale"))
			}
			result.Targets = append(result.Targets, target)
		}
	}
	sort.Slice(result.Targets, func(i, j int) bool {
		a, b := result.Targets[i].Identity, result.Targets[j].Identity
		return a.ProviderID+"\x00"+a.TargetID < b.ProviderID+"\x00"+b.TargetID
	})
	result.ReasonCodes = normalizeReasons(result.ReasonCodes)
	return result
}

func (r *Registry) Resolve(ctx context.Context, ref TargetReferenceV1, now time.Time) (TargetV1, error) {
	if err := validReference(ref); err != nil {
		return TargetV1{}, err
	}
	snapshot := r.Snapshot(ctx, now)
	if len(snapshot.ReasonCodes) != 0 {
		return TargetV1{}, errors.New("fallback_target_inventory_incomplete")
	}
	for _, target := range snapshot.Targets {
		if target.Identity.ProviderID != ref.ProviderID || target.Identity.TargetID != ref.TargetID {
			continue
		}
		if target.Readiness != ReadinessReady {
			return TargetV1{}, errors.New("fallback_target_not_ready")
		}
		if len(target.ReasonCodes) != 0 {
			return TargetV1{}, errors.New("fallback_target_has_unresolved_reasons")
		}
		if !target.Endpoint.Local || target.Endpoint.CanReachManagement != hostresources.CapabilityNo {
			return TargetV1{}, errors.New("fallback_target_not_local_or_management_isolation_unknown")
		}
		if target.PublishRevision != ref.PublishRevision || target.ContentDigest != ref.ContentDigest || target.Endpoint.EndpointID != ref.ApprovedLocalEndpointID || target.ProviderHealthRevision != ref.ProviderHealthRevision {
			return TargetV1{}, errors.New("fallback_target_reference_stale")
		}
		return target, nil
	}
	return TargetV1{}, errors.New("fallback_target_missing")
}

type LeaseStore interface {
	SaveLease(context.Context, ReferenceLeaseV1) error
	LoadLease(context.Context, string) (ReferenceLeaseV1, error)
}

type LeaseManager struct {
	Registry *Registry
	Store    LeaseStore
	Now      func() time.Time
}

func (m LeaseManager) Acquire(ctx context.Context, holder string, ref TargetReferenceV1, ttl time.Duration) (ReferenceLeaseV1, error) {
	now := m.now()
	if !validOpaqueID(holder, 128) {
		return ReferenceLeaseV1{}, errors.New("fallback lease holder is required")
	}
	if ttl <= 0 || ttl > time.Hour {
		return ReferenceLeaseV1{}, errors.New("fallback lease ttl must be within one hour")
	}
	if m.Registry == nil || m.Store == nil {
		return ReferenceLeaseV1{}, errors.New("fallback lease capability is unavailable")
	}
	if err := validReference(ref); err != nil {
		return ReferenceLeaseV1{}, err
	}
	if _, err := m.Registry.Resolve(ctx, ref, now); err != nil {
		return ReferenceLeaseV1{}, err
	}
	lease := ReferenceLeaseV1{Schema: LeaseSchemaV1, LeaseID: leaseID(holder, ref), HolderID: holder, ProviderID: ref.ProviderID, TargetID: ref.TargetID, PublishRevision: ref.PublishRevision, ContentDigest: ref.ContentDigest, ApprovedLocalEndpointID: ref.ApprovedLocalEndpointID, ProviderHealthRevision: ref.ProviderHealthRevision, IssuedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(), State: "ACTIVE"}
	if err := m.Store.SaveLease(ctx, lease); err != nil {
		return ReferenceLeaseV1{}, err
	}
	return lease, nil
}

func (m LeaseManager) Renew(ctx context.Context, leaseIDValue string, ttl time.Duration) (ReferenceLeaseV1, error) {
	now := m.now()
	if ttl <= 0 || ttl > time.Hour {
		return ReferenceLeaseV1{}, errors.New("fallback lease ttl must be within one hour")
	}
	if m.Registry == nil || m.Store == nil {
		return ReferenceLeaseV1{}, errors.New("fallback lease capability is unavailable")
	}
	lease, err := m.Store.LoadLease(ctx, leaseIDValue)
	if err != nil {
		return ReferenceLeaseV1{}, err
	}
	if lease.State != "ACTIVE" {
		return ReferenceLeaseV1{}, errors.New("fallback_target_lease_not_active")
	}
	if !lease.Fresh(now) {
		lease.State = "STALE"
		lease.ExpiresAt = now.Unix()
		lease.ReasonCodes = []string{"fallback_target_lease_stale"}
		if saveErr := m.Store.SaveLease(ctx, lease); saveErr != nil {
			return ReferenceLeaseV1{}, errors.Join(errors.New("fallback_target_lease_stale"), saveErr)
		}
		return ReferenceLeaseV1{}, errors.New("fallback_target_lease_stale")
	}
	ref := TargetReferenceV1{lease.ProviderID, lease.TargetID, lease.PublishRevision, lease.ContentDigest, lease.ApprovedLocalEndpointID, lease.ProviderHealthRevision}
	if _, err = m.Registry.Resolve(ctx, ref, now); err != nil {
		lease.State = "STALE"
		lease.ExpiresAt = now.Unix()
		lease.ReasonCodes = []string{"fallback_target_reference_stale"}
		if saveErr := m.Store.SaveLease(ctx, lease); saveErr != nil {
			return ReferenceLeaseV1{}, errors.Join(err, saveErr)
		}
		return ReferenceLeaseV1{}, err
	}
	lease.RenewedAt = now.Unix()
	lease.ExpiresAt = now.Add(ttl).Unix()
	if err = m.Store.SaveLease(ctx, lease); err != nil {
		return ReferenceLeaseV1{}, err
	}
	return lease, nil
}

func (m LeaseManager) Release(ctx context.Context, leaseIDValue string) (ReferenceLeaseV1, error) {
	if m.Store == nil {
		return ReferenceLeaseV1{}, errors.New("fallback lease capability is unavailable")
	}
	lease, err := m.Store.LoadLease(ctx, leaseIDValue)
	if err != nil {
		return ReferenceLeaseV1{}, err
	}
	now := m.now()
	lease.State = "RELEASED"
	lease.ReleasedAt = now.Unix()
	lease.ExpiresAt = now.Unix()
	if err = m.Store.SaveLease(ctx, lease); err != nil {
		return ReferenceLeaseV1{}, err
	}
	return lease, nil
}

func (m LeaseManager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func leaseID(holder string, ref TargetReferenceV1) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(holder) + "\x00" + ref.ProviderID + "\x00" + ref.TargetID))
	return "fallback-lease:" + hex.EncodeToString(sum[:16])
}

func validateTarget(target TargetV1, now time.Time) error {
	if target.Schema != TargetSchemaV1 || !validOpaqueID(target.Identity.ProviderID, 128) || !validOpaqueID(target.Identity.TargetID, 128) {
		return errors.New("target identity is invalid")
	}
	if !isSHA256(target.ContentDigest) || !validOpaqueID(target.PublishRevision, 128) || !validOpaqueID(target.ProviderHealthRevision, 128) {
		return errors.New("target revision or digest is invalid")
	}
	if !validOpaqueID(target.Endpoint.EndpointID, 128) || target.Endpoint.Port == 0 || (target.Endpoint.Network != hostresources.NetworkTCP && target.Endpoint.Network != hostresources.NetworkUDP) || (target.Endpoint.Family != hostresources.AddressFamilyIPv4 && target.Endpoint.Family != hostresources.AddressFamilyIPv6) || target.ObservedAt <= 0 || target.ObservedAt > now.UTC().Add(5*time.Minute).Unix() || target.ExpiresAt <= target.ObservedAt || target.ExpiresAt > target.ObservedAt+int64((5*time.Minute)/time.Second) || target.ConfidenceBP < 0 || target.ConfidenceBP > 10000 {
		return errors.New("target capability or time is invalid")
	}
	switch target.Readiness {
	case ReadinessReady, ReadinessNotReady, ReadinessStale, ReadinessUnknown:
	default:
		return errors.New("target readiness is invalid")
	}
	if !validOpaqueID(target.Source, 128) || !validReasons(target.ReasonCodes) {
		return errors.New("target source or reasons are invalid")
	}
	listen := hostresources.NormalizeListen(target.Endpoint.Bind)
	wantFamily := hostresources.AddressFamilyForListen(target.Endpoint.Bind)
	if wantFamily != target.Endpoint.Family || target.Endpoint.Local != (listen.Class == hostresources.ListenLoopback) {
		return errors.New("target endpoint bind or family is invalid")
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validReference(ref TargetReferenceV1) error {
	if !validOpaqueID(ref.ProviderID, 128) || !validOpaqueID(ref.TargetID, 128) || !validOpaqueID(ref.PublishRevision, 128) || !isSHA256(ref.ContentDigest) || !validOpaqueID(ref.ApprovedLocalEndpointID, 128) || !validOpaqueID(ref.ProviderHealthRevision, 128) {
		return errors.New("fallback target reference is invalid")
	}
	return nil
}

func validOpaqueID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func validReasons(values []string) bool {
	if len(values) > 32 {
		return false
	}
	for _, value := range values {
		if !validOpaqueID(value, 64) {
			return false
		}
	}
	return true
}
func normalizeReasons(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !validOpaqueID(value, 64) {
			value = "fallback_target_reason_invalid"
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) == 32 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func sanitizeTarget(target TargetV1) TargetV1 {
	invalid := false
	if !validOpaqueID(target.Identity.ProviderID, 128) && target.Identity.ProviderID != "" {
		target.Identity.ProviderID = opaqueID("provider", target.Identity.ProviderID)
		invalid = true
	}
	if !validOpaqueID(target.Identity.TargetID, 128) {
		target.Identity.TargetID = opaqueID("target", target.Identity.TargetID)
		invalid = true
	}
	if !validOpaqueID(target.PublishRevision, 128) {
		target.PublishRevision = opaqueID("publish", target.PublishRevision)
		invalid = true
	}
	if !isSHA256(target.ContentDigest) {
		target.ContentDigest = ""
		invalid = true
	}
	if !validOpaqueID(target.Endpoint.EndpointID, 128) {
		target.Endpoint.EndpointID = opaqueID("endpoint", target.Endpoint.EndpointID)
		invalid = true
	}
	if strings.ContainsAny(target.Endpoint.Bind, "/\\?#&={}[]<>\"'\r\n\t ") {
		target.Endpoint.Bind = "*"
		target.Endpoint.Family = hostresources.AddressFamilyUnknown
		invalid = true
	}
	if !validOpaqueID(target.ProviderHealthRevision, 128) {
		target.ProviderHealthRevision = opaqueID("health", target.ProviderHealthRevision)
		invalid = true
	}
	if !validOpaqueID(target.Source, 128) {
		target.Source = "unknown"
		invalid = true
	}
	target.ReasonCodes = normalizeReasons(target.ReasonCodes)
	if invalid {
		target.ReasonCodes = normalizeReasons(append(target.ReasonCodes, "fallback_target_invalid"))
	}
	return target
}

func opaqueID(prefix, value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return prefix + ":" + hex.EncodeToString(sum[:16])
}

type MemoryStore struct {
	mu     sync.Mutex
	leases map[string]ReferenceLeaseV1
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{leases: make(map[string]ReferenceLeaseV1)} }
func (s *MemoryStore) SaveLease(_ context.Context, value ReferenceLeaseV1) error {
	if s == nil {
		return fmt.Errorf("lease store unavailable")
	}
	if err := value.Validate(time.Now().UTC()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.leases[value.LeaseID]; !exists && len(s.leases) >= MaxReferenceLeases {
		return errors.New("fallback_target_lease_capacity_exceeded")
	}
	s.leases[value.LeaseID] = value
	return nil
}
func (s *MemoryStore) LoadLease(_ context.Context, id string) (ReferenceLeaseV1, error) {
	if s == nil {
		return ReferenceLeaseV1{}, fmt.Errorf("lease store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.leases[id]
	if !ok {
		return ReferenceLeaseV1{}, errors.New("fallback_target_lease_missing")
	}
	return value, nil
}

var Default = NewRegistry()

func Register(provider Provider) func() { return Default.Register(provider) }
