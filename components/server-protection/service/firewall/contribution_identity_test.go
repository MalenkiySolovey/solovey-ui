package firewall

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

// Only the observations/configuration source is injected. The neutral registries,
// canonical resource/management projection and firewall lifecycle remain real.
type renewalManagementSource struct {
	now          func() time.Time
	singleFamily bool
}

func (renewalManagementSource) Owner() string    { return "panel" }
func (renewalManagementSource) SourceID() string { return "renewal-management-fixture" }

func (s renewalManagementSource) ListProtectableResources(context.Context) ([]hostresources.ProtectableResource, error) {
	var resources []hostresources.ProtectableResource
	for _, kind := range []string{"panel_web", "subscription"} {
		for _, bind := range []string{"192.0.2.5", "2001:db8::5"} {
			if s.singleFamily && bind == "2001:db8::5" {
				continue
			}
			resource := endpointResourceFixture()
			resource.ID, resource.Kind, resource.Listen = "core:"+kind+":"+bind, kind, bind
			if kind == "subscription" {
				resource.Port = 2096
			} else {
				resource.Port = 2095
			}
			resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
			endpoint := &resource.Endpoints[0]
			endpoint.ID, endpoint.ResourceID = "endpoint:"+resource.ID, resource.ID
			endpoint.Key.BindAddress, endpoint.Key.Port = bind, uint16(resource.Port)
			endpoint.Key.AddressFamily = hostresources.AddressFamilyForListen(bind)
			endpoint.ObservedAt = s.now().Unix()
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func TestContributionIdentityDoesNotAuthorizeExpiredManagement(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	plan := endpointTemporalEndpointPlan(now, true)
	for i := range plan.Endpoints {
		plan.Endpoints[i].Contributions = nil
	}
	plan.Revision = firewallPlanRevision(plan)
	workflow, helper, _, _ := newWorkflow(t, nil)
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "expired-current-authority", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	identity := ContributionRevision(plan)
	workflow.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if ContributionRevision(plan) != identity {
		t.Fatal("elapsed time changed semantic intent")
	}
	_, err = workflow.Apply(context.Background(), ApplyInput{Plan: plan, OperationID: prepared.Operation.OperationID, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err == nil || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 0 {
		t.Fatal("unchanged semantic intent authorized expired current management/recovery evidence")
	}
	workflow.Now = func() time.Time { return now }
	for name, mutate := range map[string]func(*FirewallPlan){
		"future evidence": func(p *FirewallPlan) {
			e := *p.mutationEvidence
			e.management = append([]hostresources.ManagementEndpointV1(nil), e.management...)
			e.management[0].ObservedAt = now.Add(time.Hour).Unix()
			e.management[0].ExpiresAt = now.Add(time.Hour + time.Minute).Unix()
			p.mutationEvidence = &e
		},
		"untrusted evidence": func(p *FirewallPlan) {
			e := *p.mutationEvidence
			e.recovery = append([]hostresources.RecoveryPathV1(nil), e.recovery...)
			e.recovery[0].VerificationState = "unverified"
			p.mutationEvidence = &e
		},
		"serialized history": func(p *FirewallPlan) { data, _ := json.Marshal(p); *p = FirewallPlan{}; _ = json.Unmarshal(data, p) },
		"port":               func(p *FirewallPlan) { p.Endpoints[0].Key.Port++ },
		"bind":               func(p *FirewallPlan) { p.Endpoints[0].Key.BindAddress = "192.0.2.9" },
		"family":             func(p *FirewallPlan) { p.Endpoints[0].Key.AddressFamily = hostresources.AddressFamilyIPv6 },
		"semantic resource owner": func(p *FirewallPlan) {
			p.Resources[0].ID = "other:resource"
			p.Endpoints[0].ResourceID = "other:resource"
		},
		"policy":          func(p *FirewallPlan) { p.InputRevision = strings.Repeat("d", 64) },
		"configuration":   func(p *FirewallPlan) { p.Resources[0].Capabilities.ConfigRevision = strings.Repeat("e", 64) },
		"management keep": func(p *FirewallPlan) { p.ManagementExemptions[0].SourcePrefix = "198.51.101.0/24" },
		"candidate rules": func(p *FirewallPlan) { p.Limits.DefaultTTLSeconds++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneFirewallPlan(plan)
			mutate(&changed)
			changed.Revision = firewallPlanRevision(changed)
			_, err := workflow.Apply(context.Background(), ApplyInput{Plan: changed, OperationID: prepared.Operation.OperationID, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
			if err == nil || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 0 {
				t.Fatal("old Prepare authorized changed semantics or invalid evidence")
			}
		})
	}
}

func (s renewalManagementSource) Observe(ctx context.Context, _ hostfacts.Limits) (hostfacts.Observation, error) {
	resources, _ := s.ListProtectableResources(ctx)
	result := hostfacts.Observation{}
	for index, resource := range resources {
		fact := compositionOwnerSurface(resource, resource.Endpoints[0].Key, index, s.now())
		fact.Process.EvidenceRevision = hostresources.Revision(s.now().Unix())
		fact.ListenerOwner.Process.EvidenceRevision = fact.Process.EvidenceRevision
		fact.ListenerOwner.Seal()
		fact.ID = "surface:" + strconv.Itoa(index)
		result.Facts = append(result.Facts, fact)
	}
	return result, nil
}
