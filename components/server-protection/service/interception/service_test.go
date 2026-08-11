package interception

import (
	"context"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type factProvider struct {
	fact hostresources.InterceptionInboundFactV1
}

func (factProvider) ProviderID() string { return "core" }
func (p factProvider) InterceptionFactsV1(context.Context, time.Time) ([]hostresources.InterceptionInboundFactV1, error) {
	return []hostresources.InterceptionInboundFactV1{p.fact}, nil
}
func (factProvider) AcquireInterceptionLease(context.Context, hostresources.AcquireInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return hostresources.InterceptionLeaseV1{}, nil
}
func (factProvider) FenceInterceptionLease(context.Context, hostresources.MutateInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return hostresources.InterceptionLeaseV1{}, nil
}
func (factProvider) ActivateInterceptionLease(context.Context, hostresources.MutateInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return hostresources.InterceptionLeaseV1{}, nil
}
func (factProvider) RenewInterceptionLease(context.Context, hostresources.MutateInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return hostresources.InterceptionLeaseV1{}, nil
}
func (factProvider) ReleaseInterceptionLease(context.Context, hostresources.ReleaseInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return hostresources.InterceptionLeaseV1{}, nil
}
func (factProvider) GetInterceptionLease(context.Context, hostresources.GetInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return hostresources.InterceptionLeaseV1{}, nil
}
func (factProvider) ListInterceptionLeases(context.Context, hostresources.ListInterceptionLeasesRequestV1) ([]hostresources.InterceptionLeaseV1, error) {
	return nil, nil
}

type scopeProvider struct {
	fact hostresources.ForwardedIngressScopeFactV1
}

func (scopeProvider) ProviderID() string { return "network-owner" }
func (p scopeProvider) ForwardedIngressScopesV1(context.Context, time.Time) ([]hostresources.ForwardedIngressScopeFactV1, error) {
	return []hostresources.ForwardedIngressScopeFactV1{p.fact}, nil
}

func serviceFixture(t *testing.T) (*Service, hostresources.InterceptionInboundFactV1, time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0).UTC()
	digest := hostresources.Revision("fixture")
	fact, err := hostresources.FinalizeInterceptionFactV1(hostresources.InterceptionInboundFactV1{
		ProviderID: "core", ProviderRevision: hostresources.InterceptionProviderRevisionV1,
		ResourceID: "inbound:9", EndpointID: "endpoint:abcdef", InboundDatabaseID: 9,
		Kind: hostresources.InterceptionTProxyV1, Network: hostresources.NetworkUDP,
		AddressFamily: hostresources.AddressFamilyIPv4, ConfiguredBind: "0.0.0.0", ConfiguredPort: 15002,
		Ownership:             hostresources.InterceptionProviderManagedV1,
		ListenerState:         hostresources.InterceptionListenerObservedExactV1,
		ConfigurationRevision: digest, RuntimeRevision: digest, RuntimeGenerationRevision: digest,
		ListenerRevision: digest, CoreSemanticRevision: digest, LinuxOnly: true,
		TransparentSocketRequired: true, OriginalDestinationMechanism: "IP_RECVORIGDSTADDR",
		OriginalDestinationPreserved: true, SourcePreserved: true, PolicyRoutingRequired: true,
		BoundedUDPFlowState: true, RuntimeReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("fact: %v", err)
	}
	scope, err := hostresources.FinalizeIngressScopeFactV1(hostresources.ForwardedIngressScopeFactV1{
		ProviderID: "network-owner", ProviderRevision: hostresources.IngressScopeProviderRevisionV1,
		ScopeID: "scope:eth0:v4", InterfaceName: "eth0", InterfaceIndex: 2,
		InterfaceRevision: digest, AddressFamily: hostresources.AddressFamilyIPv4,
		Ownership: hostresources.IngressScopeProviderManagedV1, ForwardedIngress: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	interceptions := hostresources.NewInterceptionRegistryV1()
	if _, err := interceptions.Register(factProvider{fact: fact}); err != nil {
		t.Fatalf("register fact: %v", err)
	}
	scopes := hostresources.NewForwardedIngressScopeRegistryV1()
	if _, err := scopes.Register(scopeProvider{fact: scope}); err != nil {
		t.Fatalf("register scope: %v", err)
	}
	return &Service{Interceptions: interceptions, IngressScopes: scopes, Now: func() time.Time { return now }, GOOS: "linux"}, fact, now
}

func TestStatusAndPreviewRemainNonActionableDespiteExactCoreAndScope(t *testing.T) {
	service, fact, now := serviceFixture(t)
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MutationAvailable || status.DefaultEnabled || !status.Experimental || len(status.Resources) != 1 {
		t.Fatalf("unsafe status projection: %#v", status)
	}
	if status.Resources[0].Disposition != DispositionBlockedMissingCapability {
		t.Fatalf("disposition = %q", status.Resources[0].Disposition)
	}
	reference, err := hostresources.ReferenceInterceptionV1(fact, now)
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	plan, err := service.Preview(context.Background(), PreviewRequestV1{Interception: reference})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if plan.Disposition != DispositionBlockedMissingCapability || plan.ActualState != "NOT_APPLIED" ||
		plan.ManagedMark != nil || plan.RoutingTable != nil || len(plan.EligibleIngressScopes) != 1 {
		t.Fatalf("unsafe plan projection: %#v", plan)
	}
	if plan.PlanRevision != hostresources.Revision(planRevisionInputV1(plan)) {
		t.Fatal("plan revision is not deterministic")
	}
}

func TestMatrixRejectsRedirectUDPAndMutationSurface(t *testing.T) {
	service, _, _ := serviceFixture(t)
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	found := false
	for _, entry := range status.ProfileMatrix {
		if entry.Kind == hostresources.InterceptionRedirectV1 && entry.Network == hostresources.NetworkUDP {
			found = true
			if entry.Disposition != DispositionNotShipped {
				t.Fatalf("Redirect UDP disposition = %q", entry.Disposition)
			}
		}
	}
	if !found {
		t.Fatal("Redirect UDP missing from disposition matrix")
	}
	err = service.BlockedMutation(BlockedMutationRequestV1{
		PlanID: "interception-plan:fixture", ExpectedRevision: hostresources.Revision("plan"),
		IdempotencyKey: "request-1", Confirmation: mutationConfirmation,
	})
	if ErrorCode(err) != CodeMutationNotShipped {
		t.Fatalf("mutation error = %v", err)
	}
}

func TestPreviewRejectsStaleExactReference(t *testing.T) {
	service, fact, now := serviceFixture(t)
	reference, _ := hostresources.ReferenceInterceptionV1(fact, now)
	reference.RuntimeRevision = hostresources.Revision("changed")
	reference.CanonicalReferenceRevision = hostresources.Revision(struct {
		Schema, Provider, Resource, Endpoint, Runtime string
	}{reference.Schema, reference.ProviderID, reference.ResourceID, reference.EndpointID, reference.RuntimeRevision})
	if _, err := service.Preview(context.Background(), PreviewRequestV1{Interception: reference}); ErrorCode(err) != CodeMalformedInput {
		t.Fatalf("stale malformed reference error = %v", err)
	}
}
