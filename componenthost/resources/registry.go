package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultCacheTTL = 5 * time.Second
	// MaxResourceFacts bounds the neutral inventory retained and exposed by a
	// single snapshot. A truncated snapshot carries an error so consumers that
	// require completeness fail closed.
	MaxResourceFacts                  = 4096
	MaxEndpointsPerResource           = 16
	MaxAdvertisedEndpointsPerResource = 16
	maxResourceStringValues           = 32
	maxContributors                   = 128
)

type registeredContributor struct {
	id          uint64
	contributor ResourceContributor
}

type cachedSnapshot struct {
	value     ResourceSnapshot
	expiresAt time.Time
}

type Registry struct {
	mu           sync.RWMutex
	nextID       uint64
	contributors map[uint64]ResourceContributor
	cacheTTL     time.Duration
	cache        *cachedSnapshot
	now          func() time.Time
}

func NewRegistry(cacheTTL time.Duration) *Registry {
	if cacheTTL <= 0 {
		cacheTTL = defaultCacheTTL
	}
	return &Registry{
		contributors: make(map[uint64]ResourceContributor),
		cacheTTL:     cacheTTL,
		now:          time.Now,
	}
}

func (r *Registry) Register(contributor ResourceContributor) func() {
	if contributor == nil {
		return func() {}
	}
	r.mu.Lock()
	if len(r.contributors) >= maxContributors {
		r.mu.Unlock()
		panic("resource contributor registry capacity exceeded")
	}
	id := r.nextID
	r.nextID++
	r.contributors[id] = contributor
	r.cache = nil
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.contributors, id)
			r.cache = nil
			r.mu.Unlock()
		})
	}
}

func (r *Registry) Invalidate() {
	r.mu.Lock()
	r.cache = nil
	r.mu.Unlock()
}

func (r *Registry) Snapshot(ctx context.Context) ResourceSnapshot {
	return r.snapshot(ctx, false)
}

func (r *Registry) Refresh(ctx context.Context) ResourceSnapshot {
	return r.snapshot(ctx, true)
}

func (r *Registry) snapshot(ctx context.Context, refresh bool) ResourceSnapshot {
	now := r.now()
	r.mu.RLock()
	if !refresh && r.cache != nil && now.Before(r.cache.expiresAt) {
		value := cloneSnapshot(r.cache.value)
		r.mu.RUnlock()
		return value
	}
	contributors := make([]registeredContributor, 0, len(r.contributors))
	for id, contributor := range r.contributors {
		contributors = append(contributors, registeredContributor{id: id, contributor: contributor})
	}
	r.mu.RUnlock()
	sort.Slice(contributors, func(i, j int) bool { return contributors[i].id < contributors[j].id })

	snapshot := ResourceSnapshot{GeneratedAt: now.Unix(), Resources: []ProtectableResource{}}
	seen := make(map[string]string)
	truncated := false
	invalidFacts := false
	for contributorIndex, registered := range contributors {
		owner := strings.TrimSpace(registered.contributor.Owner())
		if !validResourceIdentifier(owner) {
			owner = "unknown"
		}
		items, err := listContributor(ctx, registered.contributor)
		if err != nil {
			// Contributor failures are exposed through the read-only inventory API.
			// Keep the fact that a contributor is unavailable, but never forward a
			// database/filesystem error (or a recovered panic value) across the
			// neutral boundary.
			snapshot.Errors = append(snapshot.Errors, ResourceError{Owner: owner, Message: "resource contributor is unavailable"})
			continue
		}
		for _, item := range items {
			if len(snapshot.Resources) >= MaxResourceFacts {
				truncated = true
				break
			}
			item = cloneResource(item)
			resourceInvalid := false
			item.ID = strings.TrimSpace(item.ID)
			if !validResourceIdentifier(item.ID) {
				snapshot.Warnings = append(snapshot.Warnings, ResourceWarning{Owner: owner, Code: "invalid_resource_id", Message: "contributor returned a resource without a safe stable id"})
				continue
			}
			if previousOwner, ok := seen[item.ID]; ok {
				snapshot.Warnings = append(snapshot.Warnings, ResourceWarning{Owner: owner, ResourceID: item.ID, Code: "duplicate_resource_id", Message: fmt.Sprintf("resource id is already owned by %s", previousOwner)})
				continue
			}
			if strings.TrimSpace(item.Owner) == "" {
				item.Owner = owner
			} else if item.Owner != owner {
				snapshot.Warnings = append(snapshot.Warnings, ResourceWarning{Owner: owner, ResourceID: item.ID, Code: "owner_mismatch", Message: "resource owner differs from contributor owner"})
				item.Owner = owner
			}
			originalName := strings.TrimSpace(item.Name)
			item.Name = boundedResourceLabel(item.Name, item.ID)
			resourceInvalid = resourceInvalid || (originalName != "" && item.Name != originalName)
			item.Source = safeEndpointToken(item.Source)
			if item.Source == "" {
				item.Source = "unknown"
				item.Capabilities.Known = false
				item.Warnings = append(item.Warnings, "resource source is unknown")
				invalidFacts = true
				resourceInvalid = true
			}
			originalInboundTag, originalComponentID := strings.TrimSpace(item.InboundTag), strings.TrimSpace(item.ComponentID)
			item.InboundTag = optionalResourceToken(item.InboundTag, 256)
			item.ComponentID = optionalResourceToken(item.ComponentID, 128)
			resourceInvalid = resourceInvalid || (originalInboundTag != "" && item.InboundTag == "") || (originalComponentID != "" && item.ComponentID == "")
			if invalidListenText(item.Listen) {
				item.Listen = "*"
				item.Capabilities.Known = false
				item.Warnings = append(item.Warnings, "resource listen metadata is invalid")
				invalidFacts = true
				resourceInvalid = true
			}
			normalized := NormalizeListen(item.Listen)
			item.Listen = normalized.Value
			item.Public = normalized.Public()
			item.Kind = strings.ToLower(optionalResourceToken(item.Kind, 64))
			item.Protocol = strings.ToLower(optionalResourceToken(item.Protocol, 64))
			if item.Kind == "" || item.Protocol == "" {
				resourceInvalid = true
			}
			var invalid bool
			if len(item.Capabilities.PublicHostnames) > maxResourceStringValues || len(item.Capabilities.RouteHints) > maxResourceStringValues || len(item.Warnings) > maxResourceStringValues {
				truncated = true
			}
			item.Capabilities.PublicHostnames, invalid = boundedStringFacts(item.Capabilities.PublicHostnames, true)
			invalidFacts = invalidFacts || invalid
			resourceInvalid = resourceInvalid || invalid
			item.Capabilities.RouteHints, invalid = boundedStringFacts(item.Capabilities.RouteHints, false)
			invalidFacts = invalidFacts || invalid
			resourceInvalid = resourceInvalid || invalid
			item.Warnings, invalid = boundedResourceWarnings(item.Warnings)
			invalidFacts = invalidFacts || invalid
			resourceInvalid = resourceInvalid || invalid
			if invalid {
				item.Capabilities.Known = false
			}
			for _, capability := range []CapabilityValue{item.Capabilities.AcceptsProxyProtocol, item.Capabilities.SupportsGracefulDrain, item.Capabilities.CanServeFallback, item.Capabilities.RequiresACMEHTTP01, item.Capabilities.RequiresTLSALPN01} {
				if capability != "" && normalizeCapability(capability) == CapabilityUnknown && capability != CapabilityUnknown {
					resourceInvalid = true
				}
			}
			item.Capabilities.AcceptsProxyProtocol = normalizeCapability(item.Capabilities.AcceptsProxyProtocol)
			item.Capabilities.SupportsGracefulDrain = normalizeCapability(item.Capabilities.SupportsGracefulDrain)
			item.Capabilities.CanServeFallback = normalizeCapability(item.Capabilities.CanServeFallback)
			item.Capabilities.RequiresACMEHTTP01 = normalizeCapability(item.Capabilities.RequiresACMEHTTP01)
			item.Capabilities.RequiresTLSALPN01 = normalizeCapability(item.Capabilities.RequiresTLSALPN01)
			originalTLSMode, originalFallbackTargetID := strings.TrimSpace(item.Capabilities.TLSMode), strings.TrimSpace(item.Capabilities.FallbackTargetID)
			originalOwnerRevision, originalConfigRevision := strings.TrimSpace(item.Capabilities.OwnerRevision), strings.TrimSpace(item.Capabilities.ConfigRevision)
			item.Capabilities.TLSMode = optionalResourceToken(item.Capabilities.TLSMode, 64)
			item.Capabilities.FallbackTargetID = optionalResourceToken(item.Capabilities.FallbackTargetID, 128)
			item.Capabilities.OwnerRevision = optionalResourceToken(item.Capabilities.OwnerRevision, 128)
			item.Capabilities.ConfigRevision = optionalResourceToken(item.Capabilities.ConfigRevision, 128)
			resourceInvalid = resourceInvalid || (originalTLSMode != "" && item.Capabilities.TLSMode == "") || (originalFallbackTargetID != "" && item.Capabilities.FallbackTargetID == "") || (originalOwnerRevision != "" && item.Capabilities.OwnerRevision == "") || (originalConfigRevision != "" && item.Capabilities.ConfigRevision == "")
			if expected := item.Capabilities.ExpectedListenerOwner; expected.Schema != "" && !expected.Valid() {
				item.Capabilities.ExpectedListenerOwner = ExpectedListenerOwnerV1{}
				item.Warnings = append(item.Warnings, "expected listener owner contract is invalid")
				resourceInvalid = true
			}
			if item.Port < 1 || item.Port > 65535 {
				item.Warnings = append(item.Warnings, "listener port is missing or outside 1..65535")
				resourceInvalid = true
			}
			if normalized.Class == ListenWildcard {
				item.Warnings = append(item.Warnings, "empty listen address inherits a wildcard/default bind")
			}
			if normalized.Class == ListenIPv6Wildcard {
				item.Warnings = append(item.Warnings, "IPv6 wildcard dual-stack ownership is host-dependent")
			}
			if normalized.Class == ListenHostname {
				item.Warnings = append(item.Warnings, "hostname listen requires runtime resolution before apply")
			}
			var intentValid bool
			item.ListenIntent, item.ListenIntents, intentValid = normalizeConfiguredListenIntents(item)
			if !intentValid {
				item.Warnings = append(item.Warnings, "configured listen intent is inconsistent")
				resourceInvalid = true
			}
			if len(item.Endpoints) == 0 {
				item.Endpoints = []PublicEndpoint{BuildEndpointFact(item, NetworkForProtocol(item.Protocol), now)}
			}
			if len(item.Endpoints) > MaxEndpointsPerResource {
				item.Endpoints = item.Endpoints[:MaxEndpointsPerResource]
				item.Capabilities.Known = false
				item.Warnings = append(item.Warnings, "resource endpoint inventory is truncated")
				truncated = true
			}
			for index := range item.Endpoints {
				item.Endpoints[index] = normalizeEndpointFact(item, item.Endpoints[index], now)
			}
			advertisedTruncated := len(item.AdvertisedEndpoints) > MaxAdvertisedEndpointsPerResource
			item.AdvertisedEndpoints, invalid = boundedAdvertisedEndpoints(item.AdvertisedEndpoints)
			if invalid {
				item.Capabilities.Known = false
				invalidFacts = true
				resourceInvalid = true
			}
			if advertisedTruncated {
				truncated = true
			}
			if resourceInvalid {
				item.Capabilities.Known = false
				invalidFacts = true
			}
			if resourceInvalid || truncated {
				for index := range item.Endpoints {
					if resourceInvalid {
						item.Endpoints[index].ReasonCodes = append(item.Endpoints[index].ReasonCodes, "resource_fact_invalid")
					}
					if truncated {
						item.Endpoints[index].ReasonCodes = append(item.Endpoints[index].ReasonCodes, "inventory_truncated")
					}
					item.Endpoints[index] = normalizeEndpointFact(item, item.Endpoints[index], now)
				}
			}
			item.Warnings, _ = boundedResourceWarnings(item.Warnings)
			item.Fingerprint = Fingerprint(item)
			seen[item.ID] = item.Owner
			snapshot.Resources = append(snapshot.Resources, item)
		}
		if truncated || (len(snapshot.Resources) >= MaxResourceFacts && contributorIndex+1 < len(contributors)) {
			truncated = true
			break
		}
	}
	if truncated {
		snapshot.Errors = append(snapshot.Errors, ResourceError{Owner: "registry", Message: "resource inventory is truncated"})
	}
	if invalidFacts {
		snapshot.Errors = append(snapshot.Errors, ResourceError{Owner: "registry", Message: "resource inventory contains invalid facts"})
	}
	sort.Slice(snapshot.Resources, func(i, j int) bool { return snapshot.Resources[i].ID < snapshot.Resources[j].ID })
	sort.Slice(snapshot.Warnings, func(i, j int) bool {
		left := snapshot.Warnings[i].Owner + "\x00" + snapshot.Warnings[i].ResourceID + "\x00" + snapshot.Warnings[i].Code
		right := snapshot.Warnings[j].Owner + "\x00" + snapshot.Warnings[j].ResourceID + "\x00" + snapshot.Warnings[j].Code
		return left < right
	})
	sort.Slice(snapshot.Errors, func(i, j int) bool { return snapshot.Errors[i].Owner < snapshot.Errors[j].Owner })

	r.mu.Lock()
	r.cache = &cachedSnapshot{value: cloneSnapshot(snapshot), expiresAt: now.Add(r.cacheTTL)}
	r.mu.Unlock()
	return cloneSnapshot(snapshot)
}

func validResourceIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
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

func optionalResourceToken(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !validResourceIdentifier(value) || len(value) > limit {
		return ""
	}
	return value
}

func boundedResourceLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || unsafeResourceText(value, 256) {
		return fallback
	}
	return value
}

func unsafeResourceText(value string, limit int) bool {
	value = strings.TrimSpace(value)
	return len(value) > limit || strings.ContainsAny(value, "/\\?&={}[]<>\r\n\t")
}

func invalidListenText(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) > 128 || strings.ContainsAny(value, "/\\?&={}<>\"'\r\n\t ")
}

func boundedStringFacts(values []string, hostnames bool) ([]string, bool) {
	invalid := len(values) > maxResourceStringValues
	result := make([]string, 0, min(len(values), maxResourceStringValues))
	seen := make(map[string]struct{}, len(result))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if hostnames {
			value = strings.ToLower(strings.TrimSuffix(value, "."))
		}
		if value == "" || unsafeResourceText(value, 256) {
			invalid = true
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == maxResourceStringValues {
			break
		}
	}
	sort.Strings(result)
	return result, invalid
}

func boundedResourceWarnings(values []string) ([]string, bool) {
	invalid := len(values) > maxResourceStringValues
	result := make([]string, 0, min(len(values), maxResourceStringValues))
	seen := make(map[string]struct{}, len(result))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if unsafeResourceText(value, 256) {
			value = "resource warning redacted"
			invalid = true
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == maxResourceStringValues {
			break
		}
	}
	sort.Strings(result)
	return result, invalid
}

func boundedAdvertisedEndpoints(values []AdvertisedEndpoint) ([]AdvertisedEndpoint, bool) {
	invalid := len(values) > MaxAdvertisedEndpointsPerResource
	result := make([]AdvertisedEndpoint, 0, min(len(values), MaxAdvertisedEndpointsPerResource))
	for _, value := range values {
		if !validResourceIdentifier(value.ID) || value.Port == 0 || strings.TrimSpace(value.HostnameOrIP) == "" || unsafeResourceText(value.HostnameOrIP, 256) || optionalResourceToken(value.ProtocolLabel, 64) == "" {
			invalid = true
			continue
		}
		switch value.Network {
		case NetworkTCP, NetworkUDP:
		default:
			invalid = true
			continue
		}
		var nestedInvalid bool
		value.RouteSelectors, nestedInvalid = boundedStringFacts(value.RouteSelectors, false)
		invalid = invalid || nestedInvalid
		value.SocketClaimIDs, nestedInvalid = boundedStringFacts(value.SocketClaimIDs, false)
		invalid = invalid || nestedInvalid
		value.HostnameOrIP = strings.TrimSpace(value.HostnameOrIP)
		value.ProtocolLabel = optionalResourceToken(value.ProtocolLabel, 64)
		result = append(result, value)
		if len(result) == MaxAdvertisedEndpointsPerResource {
			break
		}
	}
	return result, invalid
}

func normalizeCapability(value CapabilityValue) CapabilityValue {
	switch value {
	case CapabilityYes, CapabilityNo, CapabilityUnknown:
		return value
	default:
		return CapabilityUnknown
	}
}

func listContributor(ctx context.Context, contributor ResourceContributor) (items []ProtectableResource, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("resource contributor panicked: %v", recovered)
		}
	}()
	return contributor.ListProtectableResources(ctx)
}

func cloneSnapshot(value ResourceSnapshot) ResourceSnapshot {
	result := value
	result.Resources = append([]ProtectableResource(nil), value.Resources...)
	for index := range result.Resources {
		result.Resources[index] = cloneResource(value.Resources[index])
	}
	result.Warnings = append([]ResourceWarning(nil), value.Warnings...)
	result.Errors = append([]ResourceError(nil), value.Errors...)
	return result
}

func cloneResource(value ProtectableResource) ProtectableResource {
	result := value
	result.Warnings = append([]string(nil), value.Warnings...)
	result.Capabilities.PublicHostnames = append([]string(nil), value.Capabilities.PublicHostnames...)
	result.Capabilities.RouteHints = append([]string(nil), value.Capabilities.RouteHints...)
	result.ListenIntent.RequiredFamilies = append([]AddressFamily(nil), value.ListenIntent.RequiredFamilies...)
	result.ListenIntents = append([]ConfiguredListenIntentV1(nil), value.ListenIntents...)
	for index := range result.ListenIntents {
		result.ListenIntents[index].RequiredFamilies = append([]AddressFamily(nil), value.ListenIntents[index].RequiredFamilies...)
	}
	result.Endpoints = append([]PublicEndpoint(nil), value.Endpoints...)
	for endpoint := range result.Endpoints {
		result.Endpoints[endpoint].ReasonCodes = append([]string(nil), value.Endpoints[endpoint].ReasonCodes...)
	}
	result.AdvertisedEndpoints = append([]AdvertisedEndpoint(nil), value.AdvertisedEndpoints...)
	for endpoint := range result.AdvertisedEndpoints {
		result.AdvertisedEndpoints[endpoint].RouteSelectors = append([]string(nil), value.AdvertisedEndpoints[endpoint].RouteSelectors...)
		result.AdvertisedEndpoints[endpoint].SocketClaimIDs = append([]string(nil), value.AdvertisedEndpoints[endpoint].SocketClaimIDs...)
	}
	return result
}

var Default = NewRegistry(defaultCacheTTL)

func Register(contributor ResourceContributor) func() { return Default.Register(contributor) }
func Snapshot(ctx context.Context) ResourceSnapshot   { return Default.Snapshot(ctx) }
func Refresh(ctx context.Context) ResourceSnapshot    { return Default.Refresh(ctx) }
func Invalidate()                                     { Default.Invalidate() }
