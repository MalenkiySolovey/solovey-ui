package firewall

import (
	"context"
	"net/netip"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

// RuntimeManagement contains current owner observations only. Recovery paths,
// browser sessions and user mutation acknowledgements are not restore inputs.
type RuntimeManagement struct {
	Resources  []hostresources.ProtectableResource
	Management []hostresources.ManagementEndpointV1
	Trusted    []string
	ObservedAt time.Time
}

func (s *BaselineService) RuntimeManagement(ctx context.Context) (RuntimeManagement, error) {
	if s == nil || s.Repository == nil || s.SSH == nil {
		return RuntimeManagement{}, ErrWorkflowDisabled
	}
	ctx = s.SSH.WithCurrentPosture(ctx)
	inventory := protectionresources.Snapshot(ctx, true)
	if err := InventoryReady(inventory); err != nil {
		return RuntimeManagement{}, err
	}
	surfaces := hostfacts.Reconcile(ctx)
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	trusted, _, err := s.PolicyInputs(ctx, now)
	if err != nil {
		return RuntimeManagement{}, err
	}
	return RuntimeManagement{Resources: inventory.Resources, Management: managementregistry.Endpoints(inventory.Resources, surfaces, now), Trusted: trusted, ObservedAt: now}, nil
}

// validateRuntimeManagement proves coverage of current facts by the committed
// plan. It never rebuilds policy from the current UI or revives mutationEvidence.
func validateRuntimeManagement(plan FirewallPlan, current RuntimeManagement, now time.Time) error {
	if plan.Mode != ModeCoexistenceEndpointManaged || current.ObservedAt.IsZero() || current.ObservedAt.After(now) || now.Sub(current.ObservedAt) > 30*time.Second ||
		hostresources.Revision(CanonicalPlanResources(plan.Resources)) != hostresources.Revision(CanonicalPlanResources(current.Resources)) {
		return ErrUnsafeResource
	}
	trusted := make([]string, 0, len(current.Trusted))
	for _, source := range current.Trusted {
		prefix, err := netip.ParsePrefix(source)
		if err != nil || prefix.Masked().String() != source || prefix.Bits() != prefix.Addr().BitLen() {
			continue
		}
		trusted = append(trusted, source)
	}
	for _, exemption := range plan.ManagementExemptions {
		if exemption.RecoveryPathID != "trusted-source" {
			continue
		}
		current := false
		for _, source := range trusted {
			if source == exemption.SourcePrefix {
				current = true
				break
			}
		}
		if !current {
			return ErrUnsafeResource
		}
	}
	eligibility := evaluateFirewallBaselineEligibility(current.Resources, plan.Endpoints, current.Management, nil, trusted, protectionresources.SocketOwnershipGraph{}, true, now)
	if !eligibility.CandidateEligible {
		return ErrUnsafeResource
	}
	for _, endpoint := range current.Management {
		key := hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: hostresources.NormalizeListen(endpoint.Bind).Value, Port: endpoint.Port}
		preserved := false
		for _, exemption := range plan.ManagementExemptions {
			if exemption.RecoveryPathID != "trusted-source" || endpointKeyString(exemption.Key) != endpointKeyString(key) {
				continue
			}
			for _, source := range trusted {
				if exemption.SourcePrefix == source {
					preserved = true
				}
			}
		}
		if !preserved {
			return ErrUnsafeResource
		}
	}
	return nil
}
