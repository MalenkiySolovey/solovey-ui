//go:build !minimal

package serverprotection

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

const productionFrontingPlanCacheLimitV2 = 128

type productionFrontingPlanCacheV2 struct {
	request   protectionfronting.FrontingPreviewRequestV2
	selectors protectionfronting.SelectorSetV1
	input     protectionfronting.FrontingPlanInputV2
	plan      protectionfronting.FrontingStrategyPlanV2
}

type productionFrontingSemanticSource struct {
	backends  *hostresources.FrontingBackendRegistryV1
	fallbacks *neutralfallback.Registry
	resources func(context.Context) protectionresources.InventorySnapshot
	surfaces  func() hostfacts.Snapshot
	runtime   func(context.Context, string, time.Time) (protectionfronting.NginxRuntimeIdentityV2, error)

	mu    sync.Mutex
	cache map[string]productionFrontingPlanCacheV2
}

func newProductionFrontingSemanticSource(workflow *protectionfronting.Workflow) *productionFrontingSemanticSource {
	return &productionFrontingSemanticSource{
		backends: hostresources.DefaultFrontingBackendsV1, fallbacks: neutralfallback.Default,
		resources: func(ctx context.Context) protectionresources.InventorySnapshot {
			return protectionresources.Snapshot(ctx, false)
		},
		surfaces: hostfacts.Default.Snapshot,
		runtime: func(ctx context.Context, managementRevision string, now time.Time) (protectionfronting.NginxRuntimeIdentityV2, error) {
			return protectionfronting.ObserveManagedRuntimeIdentityV2(ctx, workflow, managementRevision, now)
		},
		cache: make(map[string]productionFrontingPlanCacheV2),
	}
}

func (s *productionFrontingSemanticSource) ResourcesV2(ctx context.Context, now time.Time) ([]protectionfronting.SemanticResourceV2, error) {
	resources, surfaces, _, runtime, inventory, backendReferences, fallbackReferences, err := s.currentFacts(ctx, now)
	if err != nil {
		return nil, err
	}
	values := make([]protectionfronting.SemanticResourceV2, 0, len(resources))
	for _, resource := range resources {
		capabilities := productionCapabilitiesV2(runtime, inventory, now)
		values = append(values, protectionfronting.SemanticResourceV2{
			ResourceID: resource.ID, DisplayIdentity: resource.Name,
			CurrentConfigurationRevision: resource.Capabilities.ConfigRevision, Runtime: runtime, Capabilities: capabilities,
			SocketClaims:       claimsForResourceV2(resource.ID, resources, surfaces, runtime.ManagementExclusionsRevision, now),
			BackendReferences:  append([]hostresources.FrontingBackendReferenceV1(nil), backendReferences...),
			FallbackReferences: append([]neutralfallback.FallbackTargetReferenceV2(nil), fallbackReferences...),
		})
	}
	return values, nil
}

func (s *productionFrontingSemanticSource) ResolvePreviewV2(ctx context.Context, request protectionfronting.FrontingPreviewRequestV2, selectors protectionfronting.SelectorSetV1, now time.Time) (protectionfronting.FrontingPlanInputV2, error) {
	input, err := s.resolveInput(ctx, request, selectors, now)
	if err != nil {
		return protectionfronting.FrontingPlanInputV2{}, err
	}
	plan, err := protectionfronting.PlanFrontingStrategyV2(input)
	if err != nil {
		return protectionfronting.FrontingPlanInputV2{}, err
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]productionFrontingPlanCacheV2)
	}
	if len(s.cache) >= productionFrontingPlanCacheLimitV2 {
		keys := make([]string, 0, len(s.cache))
		for key := range s.cache {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		delete(s.cache, keys[0])
	}
	s.cache[plan.CanonicalPlanDigest] = productionFrontingPlanCacheV2{request: request, selectors: selectors, input: input, plan: plan}
	s.mu.Unlock()
	return input, nil
}

func (s *productionFrontingSemanticSource) ResolvePrepareV2(ctx context.Context, request protectionfronting.FrontingPrepareRequestV2, now time.Time) (protectionfronting.FrontingStrategyPlanV2, error) {
	cached, ok := s.cached(request.PlanDigest)
	if !ok || cached.plan.PlanID != request.PlanID || cached.plan.PublicSocket.ResourceID != request.ResourceID {
		return protectionfronting.FrontingStrategyPlanV2{}, semanticSourceErrorV2("plan_digest_mismatch")
	}
	input, err := s.CurrentFrontingPlanInputV2(ctx, cached.plan)
	if err != nil {
		return protectionfronting.FrontingStrategyPlanV2{}, err
	}
	plan, err := protectionfronting.PlanFrontingStrategyV2(input)
	if err != nil || plan.CanonicalPlanDigest != request.PlanDigest || plan.ExpiresAt <= now.UTC().Unix() {
		return protectionfronting.FrontingStrategyPlanV2{}, semanticSourceErrorV2("plan_digest_mismatch")
	}
	return plan, nil
}

func (s *productionFrontingSemanticSource) CurrentFrontingPlanInputV2(ctx context.Context, plan protectionfronting.FrontingStrategyPlanV2) (protectionfronting.FrontingPlanInputV2, error) {
	return s.currentFrontingPlanInputV2At(ctx, plan, time.Now().UTC())
}

func (s *productionFrontingSemanticSource) currentFrontingPlanInputV2At(ctx context.Context, plan protectionfronting.FrontingStrategyPlanV2, now time.Time) (protectionfronting.FrontingPlanInputV2, error) {
	cached, ok := s.cached(plan.CanonicalPlanDigest)
	if !ok || cached.plan.CanonicalPlanDigest != plan.CanonicalPlanDigest {
		return protectionfronting.FrontingPlanInputV2{}, errors.New("plan_stale")
	}
	now = now.UTC()
	if cached.input.Runtime.Validate(now) != nil || cached.input.Socket.Validate(now) != nil {
		return protectionfronting.FrontingPlanInputV2{}, errors.New("plan_stale")
	}
	current, err := s.resolveInput(ctx, cached.request, cached.selectors, now)
	if err != nil || stableSocketRevisionV2(current.Socket) != stableSocketRevisionV2(cached.input.Socket) ||
		stableRuntimeRevisionV2(current.Runtime) != stableRuntimeRevisionV2(cached.input.Runtime) {
		return protectionfronting.FrontingPlanInputV2{}, errors.New("plan_stale")
	}
	return cached.input, nil
}

func (s *productionFrontingSemanticSource) cached(digest string) (productionFrontingPlanCacheV2, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.cache[digest]
	return value, ok
}

func (s *productionFrontingSemanticSource) resolveInput(ctx context.Context, request protectionfronting.FrontingPreviewRequestV2, selectors protectionfronting.SelectorSetV1, now time.Time) (protectionfronting.FrontingPlanInputV2, error) {
	resources, surfaces, managementRevision, runtime, inventory, _, _, err := s.currentFacts(ctx, now)
	if err != nil {
		return protectionfronting.FrontingPlanInputV2{}, err
	}
	var resource *hostresources.ProtectableResource
	for index := range resources {
		if resources[index].ID == request.ResourceID {
			resource = &resources[index]
			break
		}
	}
	if resource == nil || resource.Capabilities.ConfigRevision != request.ExpectedCurrentConfigurationRevision {
		return protectionfronting.FrontingPlanInputV2{}, semanticSourceErrorV2("configuration_revision_stale")
	}
	claims := claimsForResourceV2(resource.ID, resources, surfaces, managementRevision, now)
	var socket protectionfronting.FrontingSocketClaimV1
	for _, claim := range claims {
		if claim.EndpointID == request.SocketClaim.EndpointID && claim.ClaimRevision == request.SocketClaim.ClaimRevision {
			socket = claim
			break
		}
	}
	if socket.ClaimRevision == "" {
		return protectionfronting.FrontingPlanInputV2{}, semanticSourceErrorV2("socket_claim_stale")
	}
	backendReferences := make([]hostresources.FrontingBackendReferenceV1, 0, len(request.BackendReferences))
	for _, reference := range request.BackendReferences {
		if reference.SelectedProxyMode != request.SelectedProxyMode {
			return protectionfronting.FrontingPlanInputV2{}, semanticSourceErrorV2("target_reference_stale")
		}
		if _, resolveErr := s.backends.ResolveV1(ctx, reference, now); resolveErr != nil {
			return protectionfronting.FrontingPlanInputV2{}, semanticSourceErrorV2("target_reference_stale")
		}
		backendReferences = append(backendReferences, reference)
	}
	fallbacks := make([]protectionfronting.FallbackPlanningTargetV2, 0, len(request.FallbackReferences))
	for _, reference := range request.FallbackReferences {
		target, resolveErr := s.fallbacks.ResolveV2(ctx, reference, now)
		if resolveErr != nil {
			return protectionfronting.FrontingPlanInputV2{}, semanticSourceErrorV2("target_reference_stale")
		}
		fallbacks = append(fallbacks, protectionfronting.FallbackPlanningTargetV2{Reference: reference, Target: target})
	}
	return protectionfronting.FrontingPlanInputV2{Now: now.UTC(), DesiredStrategy: request.RequestedStrategy, Runtime: runtime, Socket: socket,
		Inventory: inventory, BackendReferences: backendReferences, FallbackTargets: fallbacks, Selectors: selectors, ProxyMode: request.SelectedProxyMode}, nil
}

func (s *productionFrontingSemanticSource) currentFacts(ctx context.Context, now time.Time) ([]hostresources.ProtectableResource, hostfacts.Snapshot, string, protectionfronting.NginxRuntimeIdentityV2, []hostresources.FrontingBackendFactV1, []hostresources.FrontingBackendReferenceV1, []neutralfallback.FallbackTargetReferenceV2, error) {
	if s == nil || s.resources == nil || s.surfaces == nil || s.backends == nil || s.fallbacks == nil {
		return nil, hostfacts.Snapshot{}, "", protectionfronting.NginxRuntimeIdentityV2{}, nil, nil, nil, semanticSourceErrorV2("validation_unavailable")
	}
	snapshot := s.resources(ctx)
	if len(snapshot.Errors) != 0 {
		return nil, hostfacts.Snapshot{}, "", protectionfronting.NginxRuntimeIdentityV2{}, nil, nil, nil, semanticSourceErrorV2("validation_unavailable")
	}
	surfaces := s.surfaceSnapshot()
	managementRevision := managementExclusionRevisionV2(snapshot.Resources)
	runtime := protectionfronting.UnknownNginxRuntimeIdentityV2(now, managementRevision, "nginx_identity_unknown")
	if s.runtime != nil {
		if observed, err := s.runtime(ctx, managementRevision, now); err == nil {
			runtime = observed
		}
	}
	inventory, inventoryErr := s.backends.FactsV1(ctx, now)
	if inventoryErr != nil {
		inventory = []hostresources.FrontingBackendFactV1{}
	}
	backendReferences := []hostresources.FrontingBackendReferenceV1{}
	for _, fact := range inventory {
		if reference, err := hostresources.ReferenceFrontingBackendV1(fact, hostresources.ProxyModeOff, now); err == nil {
			backendReferences = append(backendReferences, reference)
		}
	}
	fallbackReferences := []neutralfallback.FallbackTargetReferenceV2{}
	fallbackSnapshot := s.fallbacks.SnapshotV2(ctx, now)
	if !fallbackSnapshot.Truncated {
		for _, target := range fallbackSnapshot.Targets {
			if neutralfallback.ResolveExactV2(mustFallbackReferenceV2(target), target, now) == nil {
				fallbackReferences = append(fallbackReferences, mustFallbackReferenceV2(target))
			}
		}
	}
	return snapshot.Resources, surfaces, managementRevision, runtime, inventory, backendReferences, fallbackReferences, nil
}

func (s *productionFrontingSemanticSource) surfaceSnapshot() hostfacts.Snapshot {
	if s == nil || s.surfaces == nil {
		return hostfacts.Snapshot{}
	}
	return s.surfaces()
}

func claimsForResourceV2(resourceID string, resources []hostresources.ProtectableResource, surfaces hostfacts.Snapshot, managementRevision string, now time.Time) []protectionfronting.FrontingSocketClaimV1 {
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: resources, Surfaces: surfaces.Facts,
		InventoryTruncated: surfaces.Truncated, InventoryReasonCodes: surfaces.ReasonCodes, OwnerObservationRevision: surfaces.OwnerObservationRevision, Now: now})
	result := []protectionfronting.FrontingSocketClaimV1{}
	for _, node := range graph.Nodes {
		if node.ResourceID != resourceID {
			continue
		}
		scoped := graph
		scoped.Nodes = []protectionresources.SocketGraphNode{node}
		scoped.Collisions = nil
		scoped.ApplyBlocked = surfaces.Truncated || node.ApplyBlocked
		scoped.ReasonCodes = append([]string(nil), node.ReasonCodes...)
		for _, collision := range graph.Collisions {
			if collision.LeftResource == resourceID || collision.RightResource == resourceID {
				scoped.Collisions = append(scoped.Collisions, collision)
				scoped.ApplyBlocked = true
			}
		}
		eligibility := protectionfirewall.EvaluateListenerTopologyMutationEligibility(scoped)
		for _, desired := range node.DesiredClaims {
			if desired.Key.Network != hostresources.NetworkTCP {
				continue
			}
			var observed *protectionresources.SocketClaim
			for index := range node.ObservedClaims {
				if node.ObservedClaims[index].Key == desired.Key {
					observed = &node.ObservedClaims[index]
					break
				}
			}
			if observed == nil {
				continue
			}
			reasons := append(append([]string(nil), eligibility.ReasonCodes...), node.ReasonCodes...)
			claim, err := protectionfronting.FinalizeFrontingSocketClaimV1(protectionfronting.FrontingSocketClaimV1{
				ResourceID: resourceID, EndpointID: desired.ID, AddressFamily: desired.Key.AddressFamily, CanonicalBind: desired.Key.BindAddress,
				Wildcard: desired.Key.BindAddress == "0.0.0.0" || desired.Key.BindAddress == "::", Protocol: desired.Key.Network, PublicPort: desired.Key.Port,
				CurrentConfigurationRevision: node.ConfigurationRevision, TopologyOwnershipEligibilityRevision: eligibility.Revision,
				ListenerSocketFactRevision: stableObservedSocketFactRevisionV2(*observed), ManagementExclusionRevision: managementRevision,
				TopologyMutationEligible: eligibility.Eligible && !node.ApplyBlocked, ObservedAt: now.UTC().Unix(), ExpiresAt: now.UTC().Add(time.Minute).Unix(), ReasonCodes: reasons,
			})
			if err == nil {
				result = append(result, claim)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].EndpointID < result[j].EndpointID })
	return result
}

func productionCapabilitiesV2(runtime protectionfronting.NginxRuntimeIdentityV2, inventory []hostresources.FrontingBackendFactV1, now time.Time) []protectionfronting.NginxStrategyCapabilityV2 {
	backendProxy := protectionfronting.CapabilityUnknownV2
	for _, fact := range inventory {
		if fact.AcceptsProxyProtocol == hostresources.CapabilityYes {
			backendProxy = protectionfronting.CapabilitySupportedV2
			break
		}
	}
	return []protectionfronting.NginxStrategyCapabilityV2{
		protectionfronting.ResolveNginxStrategyCapabilityV2(runtime, protectionfronting.StrategyL4OneToOne, hostresources.ProxyModeOff, backendProxy, now),
		protectionfronting.ResolveNginxStrategyCapabilityV2(runtime, protectionfronting.StrategySNIPreread, hostresources.ProxyModeOff, backendProxy, now),
		protectionfronting.ResolveNginxStrategyCapabilityV2(runtime, protectionfronting.StrategyHTTPTerminating, hostresources.ProxyModeOff, backendProxy, now),
		protectionfronting.ResolveNginxStrategyCapabilityV2(runtime, protectionfronting.StrategyUDPQUIC, hostresources.ProxyModeOff, backendProxy, now),
	}
}

func managementExclusionRevisionV2(resources []hostresources.ProtectableResource) string {
	type binding struct{ ID, Kind, ConfigurationRevision string }
	values := []binding{}
	for _, resource := range resources {
		if resource.Kind == "panel_web" || resource.Kind == "subscription" {
			values = append(values, binding{resource.ID, resource.Kind, resource.Capabilities.ConfigRevision})
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return hostresources.Revision(struct {
		Schema string
		Items  []binding
	}{"solovey-ui/fronting-management-exclusions/v2", values})
}

func stableRuntimeRevisionV2(value protectionfronting.NginxRuntimeIdentityV2) string {
	value.ObservedAt, value.ExpiresAt, value.CanonicalRuntimeIdentityRevision = 0, 0, ""
	return hostresources.Revision(value)
}

func stableSocketRevisionV2(value protectionfronting.FrontingSocketClaimV1) string {
	value.ObservedAt, value.ExpiresAt, value.ClaimRevision = 0, 0, ""
	return hostresources.Revision(value)
}

func stableObservedSocketFactRevisionV2(value protectionresources.SocketClaim) string {
	value.ObservedAt, value.ExpiresAt = 0, 0
	return hostresources.Revision(value)
}

func mustFallbackReferenceV2(target neutralfallback.FallbackTargetV2) neutralfallback.FallbackTargetReferenceV2 {
	reference, _ := neutralfallback.ReferenceV2FromTarget(target)
	return reference
}

func semanticSourceErrorV2(code string) error { return &protectionfronting.SemanticErrorV2{Code: code} }

var _ protectionfronting.SemanticSourceV2 = (*productionFrontingSemanticSource)(nil)
var _ protectionfronting.PlanSourceV2 = (*productionFrontingSemanticSource)(nil)
