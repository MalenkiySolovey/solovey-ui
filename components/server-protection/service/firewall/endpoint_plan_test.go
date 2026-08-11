package firewall

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func TestEndpointPlanDeterministicConservativeCandidate(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := endpointResourceFixture()
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{endpointSurfaceFixture(resource, now)}, Now: now})
	management := hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:panel", Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "192.0.2.5", Port: 443, ServiceKind: hostresources.ManagementPanel, Exposure: hostresources.EndpointIntentPublic, Owner: "panel", ResourceID: resource.ID, RecoveryPolicy: "fresh_independent_path_required", Source: "fixture", ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: strings.Repeat("c", 64)}
	recovery := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:panel", Kind: string(hostresources.ManagementPanel), EndpointID: management.ID, PrincipalID: "principal:hash", SourcePrefix: "198.51.100.0/24", VerificationMethod: "fresh_panel_login", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", SourceRevision: strings.Repeat("a", 64), ConfigurationRevision: management.ConfigurationRevision}
	actions := []EndpointActionInput{
		endpointActionFixture(graph, resource, strings.Repeat("a", 64), domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"}, domain.IntentTemporaryBlock, now, now.Add(10*time.Minute), "native"),
		endpointActionFixture(graph, resource, strings.Repeat("c", 64), domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"}, domain.IntentTemporaryBlock, now, now.Add(8*time.Minute), "external"),
		endpointActionFixture(graph, resource, strings.Repeat("b", 64), domain.SignalSubjectV2{Type: "prefix", Value: "203.0.113.0/24"}, domain.IntentSoftGraylist, now, now.Add(5*time.Minute), "native"),
	}
	input := EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Management: []hostresources.ManagementEndpointV1{management}, RecoveryPaths: []hostresources.RecoveryPathV1{recovery}, TrustedSources: []string{"198.51.100.0/24"}, Actions: actions, Now: now}
	first := BuildEndpointPlan(input)
	slices.Reverse(input.Actions)
	second := BuildEndpointPlan(input)
	if first.Revision != second.Revision || RenderManagedNFT(first) != RenderManagedNFT(second) {
		t.Fatal("endpoint plan or nft candidate changed with input order")
	}
	if err := Preflight(first); err != nil {
		t.Fatalf("exact endpoint plan failed preflight: %v\n%s", err, RenderManagedNFT(first))
	}
	candidate := RenderManagedNFT(first)
	want, err := os.ReadFile("testdata/endpoint_ipv4_tcp.golden.nft")
	if err != nil {
		t.Fatal(err)
	}
	if candidate != string(want) {
		t.Fatalf("endpoint nftables candidate differs from golden:\n--- want ---\n%s--- got ---\n%s", want, candidate)
	}
	ordered := []string{
		`comment "solovey-revision:` + first.Revision + `"`,
		"flags interval,timeout",
		"type filter hook input priority -5; policy accept;",
		"meta nfproto ipv4 ip saddr 198.51.100.0/24 ip daddr 192.0.2.5 meta l4proto tcp tcp dport 443 counter accept",
		`iifname "lo" counter accept`,
		"ct state established,related counter accept",
		"limit rate over 5/second burst 10 packets counter drop",
		"counter drop",
	}
	last := -1
	for _, fragment := range ordered {
		index := strings.Index(candidate, fragment)
		if index < 0 || index <= last {
			t.Fatalf("candidate is missing conservative ordered fragment %q:\n%s", fragment, candidate)
		}
		last = index
	}
	for _, forbidden := range []string{"flush ruleset", "table ip ", "table ip6 ", "docker", "iptables", "include ", "define "} {
		if strings.Contains(strings.ToLower(candidate), forbidden) {
			t.Fatalf("candidate contains forbidden token %q", forbidden)
		}
	}
	if first.Endpoints[0].DesiredStatus != "MANAGED_ENDPOINT" || first.Endpoints[0].SelectedStatus != "PLANNED" || first.Endpoints[0].ActualStatus != "NOT_APPLIED" {
		t.Fatalf("desired/selected/actual status is dishonest: %#v", first.Endpoints[0])
	}
	refCounted := false
	for _, contribution := range first.Endpoints[0].Contributions {
		refCounted = refCounted || contribution.Intent == domain.IntentTemporaryBlock && contribution.RefCount == 2 && len(contribution.ActionIDs) == 2 && len(contribution.DecisionIDs) == 2
	}
	if len(first.Endpoints[0].Contributions) != 2 || !refCounted {
		t.Fatalf("duplicate materialization was not ref-counted: %#v", first.Endpoints[0].Contributions)
	}
	for index := 1; index < len(first.Endpoints[0].Contributions); index++ {
		previous := first.Endpoints[0].Contributions[index-1]
		current := first.Endpoints[0].Contributions[index]
		if previous.Subject+"\x00"+string(previous.Intent) > current.Subject+"\x00"+string(current.Intent) {
			t.Fatalf("endpoint contributions are not deterministically ordered: %#v", first.Endpoints[0].Contributions)
		}
	}
}

func TestEndpointPlanRevisionExcludesObservationClockButBindsSemanticInputs(t *testing.T) {
	now := time.Unix(2_000, 0).UTC()
	resource := endpointResourceFixture()
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{endpointSurfaceFixture(resource, now)}, Now: now})
	base := EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Now: now}
	first := BuildEndpointPlan(base)
	observed := resource
	observed.Endpoints = append([]hostresources.PublicEndpoint(nil), resource.Endpoints...)
	for index := range observed.Endpoints {
		observed.Endpoints[index].ObservedAt += 123
	}
	second := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{observed}, Now: now.Add(123 * time.Second)})
	if first.Revision != second.Revision || first.InputRevision != second.InputRevision {
		t.Fatal("volatile endpoint observation time contaminated the semantic revision")
	}
	changed := base
	changed.InputRevision = strings.Repeat("d", 64)
	third := BuildEndpointPlan(changed)
	changed.InputRevision = strings.Repeat("e", 64)
	fourth := BuildEndpointPlan(changed)
	if third.Revision == fourth.Revision || third.InputRevision == fourth.InputRevision {
		t.Fatal("snapshot/runtime binding change did not change the candidate plan")
	}
}

func TestEndpointPlanRevisionExcludesListenerOwnerAndDeploymentEvidence(t *testing.T) {
	now := time.Unix(2_500, 0).UTC()
	firstResource := endpointResourceFixture()
	firstGraph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{firstResource}, Surfaces: []hostsurface.HostSurfaceFactV1{endpointSurfaceFixture(firstResource, now)}, Now: now})
	action := endpointActionFixture(firstGraph, firstResource, strings.Repeat("a", 64), domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"}, domain.IntentTemporaryBlock, now, now.Add(time.Hour), "native")
	first := BuildEndpointPlan(EndpointPlanInput{Graph: firstGraph, Resources: []hostresources.ProtectableResource{firstResource}, Actions: []EndpointActionInput{action}, Now: now})

	changedResource := firstResource
	changedResource.Owner = "replacement-owner"
	changedResource.Capabilities.OwnerRevision = strings.Repeat("d", 64)
	changedResource.Capabilities.ExpectedListenerOwner.DeploymentID = "dep-" + strings.Repeat("8", 64)
	changedResource.Capabilities.ExpectedListenerOwner.ExecutableSHA256 = strings.Repeat("9", 64)
	changedResource.Endpoints = append([]hostresources.PublicEndpoint(nil), firstResource.Endpoints...)
	changedResource.Endpoints[0].Owner = changedResource.Owner
	changedResource.Endpoints[0].OwnerRevision = changedResource.Capabilities.OwnerRevision
	unknownSurface := endpointSurfaceFixture(firstResource, now)
	unknownSurface.Classification = hostsurface.ClassificationUnknownOwner
	unknownSurface.OwnershipMode = hostsurface.OwnershipUnmanaged
	unknownSurface.DesiredOwner = ""
	secondGraph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{changedResource}, Surfaces: []hostsurface.HostSurfaceFactV1{unknownSurface}, Now: now})
	second := BuildEndpointPlan(EndpointPlanInput{Graph: secondGraph, Resources: []hostresources.ProtectableResource{changedResource}, Actions: []EndpointActionInput{action}, Now: now})

	if firstGraph.Revision == secondGraph.Revision {
		t.Fatal("fixture did not change listener-owner graph evidence")
	}
	if first.InputRevision != second.InputRevision || first.Revision != second.Revision || RenderManagedNFT(first) != RenderManagedNFT(second) {
		t.Fatal("listener owner or deployment evidence contaminated the firewall baseline candidate")
	}
	if !slices.Contains(second.BaselineEligibility.AdvisoryCodes, "resource_apply_blocked") {
		t.Fatalf("unknown listener ownership was not retained as advisory evidence: %#v", second.BaselineEligibility)
	}
	if !EvaluateListenerTopologyMutationEligibility(firstGraph).Eligible || EvaluateListenerTopologyMutationEligibility(secondGraph).Eligible {
		t.Fatal("exact listener ownership was not retained as a separate topology-mutation gate")
	}
}

func TestEndpointPlanInputRevisionIsOrderIndependentAndRejectsMalformedFence(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	firstResource := endpointResourceFixture()
	secondResource := firstResource
	secondResource.ID = "fixture:listener:two"
	secondResource.Fingerprint = strings.Repeat("f", 64)
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{firstResource, secondResource}, Now: now})
	input := EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{firstResource, secondResource}, TrustedSources: []string{"198.51.100.0/24", "192.0.2.0/24"}, Now: now}
	first := EndpointInputRevision(input)
	slices.Reverse(input.Resources)
	slices.Reverse(input.TrustedSources)
	if second := EndpointInputRevision(input); first != second {
		t.Fatal("semantic input revision depends on input order")
	}
	input.InputRevision = "malformed"
	plan := BuildEndpointPlan(input)
	if !plan.ApplyBlocked || !slices.Contains(plan.ReasonCodes, "snapshot_input_revision_invalid") || Preflight(plan) == nil {
		t.Fatal("malformed snapshot fence did not fail closed")
	}
}

func TestEndpointPlanBlocksStaleActionsWrongFamilyAndAmbiguousResourceScope(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := endpointResourceFixture()
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{endpointSurfaceFixture(resource, now)}, Now: now})

	stale := endpointActionFixture(graph, resource, strings.Repeat("a", 64), domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"}, domain.IntentTemporaryBlock, now, now.Add(time.Hour), "native")
	stale.Action.ConfigurationRevision = strings.Repeat("e", 64)
	stale.Action.FinalizeID()
	stalePlan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Actions: []EndpointActionInput{stale}, Now: now})
	if !stalePlan.ApplyBlocked || !slices.Contains(stalePlan.ReasonCodes, "action_revision_stale") || Preflight(stalePlan) == nil {
		t.Fatalf("stale action revision was materialized: %#v", stalePlan)
	}

	wrongFamily := endpointActionFixture(graph, resource, strings.Repeat("b", 64), domain.SignalSubjectV2{Type: "ip", Value: "2001:db8::10"}, domain.IntentTemporaryBlock, now, now.Add(time.Hour), "native")
	wrongFamilyPlan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Actions: []EndpointActionInput{wrongFamily}, Now: now})
	if err := Preflight(wrongFamilyPlan); !errors.Is(err, ErrUnsafeResource) {
		t.Fatalf("cross-family contribution passed preflight: %v", err)
	}

	second := resource.Endpoints[0]
	second.ID = "endpoint:panel:alternate"
	second.Key.Port = 8443
	resource.Endpoints = append(resource.Endpoints, second)
	surfaceOne := endpointSurfaceFixture(resource, now)
	surfaceTwo := surfaceOne
	surfaceTwo.ID = "surface:panel:alternate"
	surfaceTwo.Port = 8443
	surfaceTwo.SocketInode = "101"
	multiGraph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surfaceOne, surfaceTwo}, Now: now})
	multiPlan := BuildEndpointPlan(EndpointPlanInput{Graph: multiGraph, Resources: []hostresources.ProtectableResource{resource}, Now: now})
	if multiPlan.ApplyBlocked || !multiPlan.BaselineEligibility.CandidateEligible || !slices.Contains(multiPlan.BaselineEligibility.AdvisoryCodes, "resource_apply_blocked") || Preflight(multiPlan) != nil {
		t.Fatalf("topology diagnostics incorrectly blocked the configured firewall baseline: %#v", multiPlan)
	}
}

func TestEndpointPlanUnknownOwnerIsAdvisoryButFreshRecoveryStillGatesMutation(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := endpointResourceFixture()
	surface := endpointSurfaceFixture(resource, now)
	surface.Classification = hostsurface.ClassificationUnknownOwner
	surface.OwnershipMode = hostsurface.OwnershipUnmanaged
	surface.DesiredOwner = ""
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
	management := hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:panel", Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "192.0.2.5", Port: 443, ServiceKind: hostresources.ManagementPanel, ResourceID: resource.ID, ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: resource.Capabilities.ConfigRevision}
	input := EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Management: []hostresources.ManagementEndpointV1{management}, TrustedSources: []string{"198.51.100.0/24"}, Now: now}
	plan := BuildEndpointPlan(input)
	if plan.ApplyBlocked || !plan.BaselineEligibility.CandidateEligible || plan.BaselineEligibility.MutationReady || !slices.Contains(plan.BaselineEligibility.AdvisoryCodes, "resource_apply_blocked") || !slices.Contains(plan.BaselineEligibility.MutationReasonCodes, "fresh_recovery_path_missing") {
		t.Fatalf("unknown owner was not advisory or recovery was not deferred to mutation: %#v", plan)
	}
	if err := Preflight(plan); err == nil {
		t.Fatal("endpoint plan without fresh recovery passed mutation preflight")
	}
	input.RecoveryPaths = []hostresources.RecoveryPathV1{{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:panel", Kind: string(hostresources.ManagementPanel), EndpointID: management.ID, PrincipalID: "principal:hash", SourcePrefix: "198.51.100.0/24", VerificationMethod: "fresh_panel_login", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), IndependenceClass: "independent_reconnect", VerificationState: "verified", SourceRevision: strings.Repeat("a", 64), ConfigurationRevision: management.ConfigurationRevision}}
	ready := BuildEndpointPlan(input)
	if ready.ApplyBlocked || !ready.BaselineEligibility.CandidateEligible || !ready.BaselineEligibility.MutationReady || Preflight(ready) != nil {
		t.Fatalf("fresh recovery did not make the owner-advisory baseline mutation-ready: %#v", ready)
	}
}

func TestEndpointPlanRequiresCurrentSSHAndFamilyScopedTrustedSource(t *testing.T) {
	now := time.Unix(1_500, 0).UTC()
	resource := endpointResourceFixture()
	ssh := hostresources.ManagementEndpointV1{
		Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:ssh:primary",
		Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "0.0.0.0", Port: 22,
		ServiceKind: hostresources.ManagementSSH, ResourceID: "core:ssh:primary", Source: "host-surface",
		ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: strings.Repeat("e", 64),
	}
	wrongFamily := BuildEndpointPlan(EndpointPlanInput{Resources: []hostresources.ProtectableResource{resource}, Management: []hostresources.ManagementEndpointV1{ssh}, TrustedSources: []string{"2001:db8::/64"}, RequireSSHKeep: true, Now: now})
	if wrongFamily.BaselineEligibility.CandidateEligible || !slices.Contains(wrongFamily.BaselineEligibility.ReasonCodes, "management_trusted_source_missing") {
		t.Fatalf("cross-family trusted source preserved IPv4 SSH: %#v", wrongFamily.BaselineEligibility)
	}

	current := BuildEndpointPlan(EndpointPlanInput{Resources: []hostresources.ProtectableResource{resource}, Management: []hostresources.ManagementEndpointV1{ssh}, TrustedSources: []string{"198.51.100.0/24"}, RequireSSHKeep: true, Now: now})
	if current.ApplyBlocked || !current.BaselineEligibility.CandidateEligible || current.BaselineEligibility.MutationReady || !slices.Contains(current.BaselineEligibility.MutationReasonCodes, "fresh_recovery_path_missing") {
		t.Fatalf("current scoped SSH keep was rejected or incorrectly mutation-ready: %#v", current.BaselineEligibility)
	}

	ssh.ReasonCodes = []string{"stale"}
	stale := BuildEndpointPlan(EndpointPlanInput{Resources: []hostresources.ProtectableResource{resource}, Management: []hostresources.ManagementEndpointV1{ssh}, TrustedSources: []string{"198.51.100.0/24"}, RequireSSHKeep: true, Now: now})
	if stale.BaselineEligibility.CandidateEligible || !slices.Contains(stale.BaselineEligibility.ReasonCodes, "management_endpoint_inventory_incomplete") {
		t.Fatalf("stale SSH surface was accepted: %#v", stale.BaselineEligibility)
	}
}

func TestEndpointPlanUsesExistingFencedHelperWorkflow(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := endpointResourceFixture()
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{endpointSurfaceFixture(resource, now)}, Now: now})
	plan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Actions: []EndpointActionInput{endpointActionFixture(graph, resource, strings.Repeat("a", 64), domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"}, domain.IntentTemporaryBlock, now, now.Add(time.Hour), "native")}, Now: now})
	workflow, _, _, _ := newFakeCIWorkflow(t, nil)
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "endpoint-baseline", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	stale := plan
	stale.Revision = strings.Repeat("d", 64)
	if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: stale, Resources: stale.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}); !errors.Is(err, ErrPlanRevision) {
		t.Fatalf("stale endpoint revision was not fenced: %v", err)
	}
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "applied" || result.ActualStatus != "APPLIED" || result.CandidateSHA256 == "" || result.RollbackSHA256 == "" {
		t.Fatalf("existing helper/journal workflow did not verify exact endpoint candidate: %#v", result)
	}
	replayed, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ActualStatus != "UNKNOWN" || !slices.Contains(replayed.ReasonCodes, "actual_state_reverification_required") {
		t.Fatalf("persisted APPLIED state was false-claimed as current actual state: %#v", replayed)
	}
}

func TestEndpointPlanRendersIPv6UDPAsDistinctExactObject(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	endpoint := hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:udp6", Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkUDP, AddressFamily: hostresources.AddressFamilyIPv6, BindAddress: "2001:db8::5", Port: 443}, Intent: hostresources.EndpointIntentPublic, Protocol: "udp", ResourceID: "core:inbound:udp6", Owner: "sing-box", OwnerRevision: strings.Repeat("b", 64), ConfigurationRevision: strings.Repeat("c", 64), ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10000}
	resource := hostresources.ProtectableResource{ID: endpoint.ResourceID, Kind: "inbound", Owner: endpoint.Owner, Protocol: "udp", Listen: endpoint.Key.BindAddress, Port: int(endpoint.Key.Port), Public: true, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: endpoint.OwnerRevision, ConfigRevision: endpoint.ConfigurationRevision}, Endpoints: []hostresources.PublicEndpoint{endpoint}}
	pid := 101
	surface := hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:udp6", Network: hostsurface.NetworkUDP, Family: hostsurface.FamilyIPv6, Bind: endpoint.Key.BindAddress, Port: endpoint.Key.Port, Exposure: hostsurface.ExposurePublic, SocketInode: "101", Process: hostsurface.ProcessFact{PID: &pid, StartTime: "1", ExeDigest: strings.Repeat("a", 64)}, RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipManaged, LastSeen: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: endpoint.ConfigurationRevision, Classification: hostsurface.ClassificationExpectedManaged}
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
	plan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Actions: []EndpointActionInput{endpointActionFixture(graph, resource, strings.Repeat("a", 64), domain.SignalSubjectV2{Type: "ip", Value: "2001:db8::10"}, domain.IntentRateLimit, now, now.Add(time.Hour), "native")}, Now: now})
	candidate := RenderManagedNFT(plan)
	for _, fragment := range []string{"type ipv6_addr", "meta nfproto ipv6 ip6 daddr 2001:db8::5 meta l4proto udp udp dport 443", "ip6 saddr @solovey_rate6_"} {
		if !strings.Contains(candidate, fragment) {
			t.Fatalf("IPv6/UDP endpoint candidate omitted %q:\n%s", fragment, candidate)
		}
	}
	if strings.Contains(candidate, "meta l4proto tcp") || strings.Contains(candidate, "type ipv4_addr") {
		t.Fatalf("IPv6/UDP endpoint was widened to TCP/IPv4:\n%s", candidate)
	}
}

func endpointResourceFixture() hostresources.ProtectableResource {
	ownerRevision, configRevision := strings.Repeat("b", 64), strings.Repeat("c", 64)
	expected := hostresources.ExpectedListenerOwnerV1{
		Schema: hostresources.ExpectedListenerOwnerSchemaV1, ContractRevision: strings.Repeat("a", 64),
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("2", 64),
		ArtifactRevision: "art-" + strings.Repeat("3", 64), DeploymentID: "dep-" + strings.Repeat("4", 64),
		RuntimeRootBindingRevision: strings.Repeat("5", 64), ServiceIdentity: "solovey-ui.panel",
		SystemdUnit: "solovey-ui.service", ServiceFragmentPath: "/etc/systemd/system/solovey-ui.service",
		ServiceUnitSHA256: strings.Repeat("7", 64), ServiceControlGroup: "/system.slice/solovey-ui.service",
		ExecutablePath: "/usr/local/bin/solovey-ui", ExecutableSHA256: strings.Repeat("6", 64),
	}
	endpoint := hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:panel", Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "192.0.2.5", Port: 443}, Intent: hostresources.EndpointIntentPublic, Protocol: "tcp", ResourceID: "core:panel:web", Owner: "panel", OwnerRevision: ownerRevision, ConfigurationRevision: configRevision, ObservedAt: 1000, Source: "fixture", ConfidenceBP: 10000}
	resource := hostresources.ProtectableResource{ID: "core:panel:web", Kind: "inbound", Owner: "panel", Protocol: "tcp", Listen: "192.0.2.5", Port: 443, Public: true, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: ownerRevision, ConfigRevision: configRevision, ExpectedListenerOwner: expected}, Endpoints: []hostresources.PublicEndpoint{endpoint}}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	return resource
}

func endpointSurfaceFixture(resource hostresources.ProtectableResource, now time.Time) hostsurface.HostSurfaceFactV1 {
	expected := resource.Capabilities.ExpectedListenerOwner
	process := hostsurface.ProcessFact{PID: endpointIntPtr(100), ParentPID: endpointIntPtr(1), SessionID: endpointIntPtr(100), StartTime: "1000", ExeDigest: expected.ExecutableSHA256, Executable: "/usr/local/bin/solovey-ui", ExeDevice: 1, ExeInode: 2, UID: endpointIntPtr(0), GID: endpointIntPtr(0), ControlGroup: "/system.slice/solovey-ui.service"}
	service := hostsurface.ServiceFact{SystemdUnit: expected.SystemdUnit, MainPID: endpointIntPtr(100), FragmentPath: "/etc/systemd/system/solovey-ui.service", FragmentSHA256: expected.ServiceUnitSHA256, ActiveState: "active", SubState: "running", ControlGroup: process.ControlGroup, StartMonotonicUsec: 100}
	fact := hostsurface.ListenerOwnerFactV1{
		Schema:  hostsurface.ListenerOwnerFactSchemaV1,
		Socket:  hostsurface.ListenerSocketIdentityV1{Network: hostsurface.NetworkTCP, Family: hostsurface.FamilyIPv4, Bind: "192.0.2.5", Port: 443, Inode: "100", Cookie: 101, CoverageFamilies: []hostsurface.Family{hostsurface.FamilyIPv4}},
		Process: process, Service: service,
		Application: hostsurface.ListenerApplicationIdentityV1{InstanceID: expected.InstanceID, SourceRevision: expected.SourceRevision, ArtifactRevision: expected.ArtifactRevision, DeploymentID: expected.DeploymentID, OwnerContractRevision: expected.ContractRevision, RuntimeRootBindingRevision: expected.RuntimeRootBindingRevision, ExpectedExecutableSHA256: expected.ExecutableSHA256, ServiceIdentity: expected.ServiceIdentity, ResourceID: resource.ID, ResourceOwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision},
		ObservedAt:  now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	fact.Seal()
	return hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:panel", Network: fact.Socket.Network, Family: fact.Socket.Family, Bind: fact.Socket.Bind, Port: fact.Socket.Port, Exposure: hostsurface.ExposurePublic, SocketInode: fact.Socket.Inode, SocketCookie: fact.Socket.Cookie, Process: process, Service: service, ListenerOwner: &fact, RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipManaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: fact.ExpiresAt, Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: resource.Capabilities.ConfigRevision, Classification: hostsurface.ClassificationManagedExact}
}

func endpointIntPtr(value int) *int { return &value }

func endpointActionFixture(graph protectionresources.SocketOwnershipGraph, resource hostresources.ProtectableResource, decisionID string, subject domain.SignalSubjectV2, intent domain.ResponseIntent, now, expires time.Time, sourceClass string) EndpointActionInput {
	for _, node := range graph.Nodes {
		if node.ResourceID != resource.ID || len(node.DesiredClaims) != 1 {
			continue
		}
		action := domain.AppliedActionV1{Schema: domain.AppliedActionSchemaV1, DecisionID: decisionID, PlanDigest: strings.Repeat("f", 64), ResourceID: resource.ID, Subject: subject, GraphRevision: EndpointActionScopeRevision([]hostresources.ProtectableResource{resource}), EndpointRevision: configuredEndpointRevision(resource, node.DesiredClaims[0].Key, baselineStrategy(resource)), ResourceRevision: EndpointActionResourceRevision(resource), ConfigurationRevision: resource.Capabilities.ConfigRevision, RequestedIntent: intent, ResolvedIntent: intent, DesiredState: "REQUESTED", SelectedState: "SELECTED", ActualState: "NOT_APPLIED", State: domain.ActionPlanned, CreatedAt: now, ExpiresAt: expires}
		action.FinalizeID()
		return EndpointActionInput{Action: action, SourceClass: sourceClass}
	}
	return EndpointActionInput{}
}
