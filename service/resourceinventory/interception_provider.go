package resourceinventory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/gorm"
)

const coreInterceptionProviderIDV1 = "core"

type CoreInterceptionProviderV1 struct {
	db        *gorm.DB
	now       func() time.Time
	snapshots func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error)
	surfaces  func() hostsurface.Snapshot
}

func NewCoreInterceptionProviderV1(db *gorm.DB, control *coreinboundcontrol.Service) *CoreInterceptionProviderV1 {
	provider := &CoreInterceptionProviderV1{
		db: db, now: time.Now, surfaces: hostsurface.CurrentSnapshot,
	}
	if control != nil {
		provider.snapshots = control.ListSnapshots
	}
	return provider
}

func (*CoreInterceptionProviderV1) ProviderID() string { return coreInterceptionProviderIDV1 }

type coreInterceptionFactV1 struct {
	fact      hostresources.InterceptionInboundFactV1
	inboundID uint
}

func (p *CoreInterceptionProviderV1) InterceptionFactsV1(ctx context.Context, now time.Time) ([]hostresources.InterceptionInboundFactV1, error) {
	values, err := p.interceptionFacts(ctx, now)
	if err != nil {
		return nil, err
	}
	result := make([]hostresources.InterceptionInboundFactV1, 0, len(values))
	for _, value := range values {
		result = append(result, value.fact)
	}
	return result, nil
}

func (p *CoreInterceptionProviderV1) interceptionFacts(ctx context.Context, now time.Time) ([]coreInterceptionFactV1, error) {
	if p == nil || p.db == nil || p.snapshots == nil {
		return nil, errors.New("core_interception_provider_unavailable")
	}
	now = now.UTC()
	if now.IsZero() {
		now = p.currentTime()
	}
	snapshots, err := p.snapshots(ctx, hostresources.MaxResourceFacts+1)
	if err != nil || len(snapshots) > hostresources.MaxResourceFacts {
		return nil, errors.New("core_interception_inventory_unavailable")
	}
	surfaceSnapshot := hostsurface.Snapshot{}
	if p.surfaces != nil {
		surfaceSnapshot = p.surfaces()
	}
	result := make([]coreInterceptionFactV1, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !snapshot.Interception.Candidate {
			continue
		}
		kind := hostresources.InterceptionKindV1(strings.ToUpper(snapshot.Interception.Kind))
		if kind != hostresources.InterceptionRedirectV1 && kind != hostresources.InterceptionTProxyV1 {
			continue
		}
		resource := inboundResourceAt(snapshot, now)
		if resource.Kind != "inbound" || resource.Owner != p.ProviderID() {
			continue
		}
		for _, endpoint := range resource.Endpoints {
			if !interceptionNetwork(snapshot.Interception.EffectiveNetworks, endpoint.Key.Network) {
				continue
			}
			observed, listenerState, ownership, reasons := exactInterceptionSurface(surfaceSnapshot, resource.ID, endpoint, now)
			runtimeReady := snapshot.Effective.RuntimeAvailable && snapshot.Effective.Present &&
				snapshot.Effective.ConfigurationProven && len(snapshot.Effective.ReasonCodes) == 0
			if !runtimeReady {
				reasons = append(reasons, "RUNTIME_CONFIGURATION_UNPROVEN")
			}
			if endpoint.Key.AddressFamily == hostresources.AddressFamilyUnknown {
				reasons = append(reasons, "LISTENER_FAMILY_UNKNOWN")
			}
			listenerRevision := hostresources.Revision(struct {
				Schema     string
				EndpointID string
				Owner      string
				Observed   string
			}{"solovey-ui/core-interception-listener/v1", endpoint.ID, resource.Capabilities.OwnerRevision, observedRevision(observed)})
			runtimeRevision := snapshot.Effective.Revision
			if runtimeRevision == "" {
				runtimeRevision = hostresources.Revision("solovey-ui/core-interception-runtime-unavailable/v1")
			}
			generationRevision := snapshot.RuntimeIdentityRevision
			if generationRevision == "" {
				generationRevision = hostresources.Revision("solovey-ui/core-interception-generation-unavailable/v1")
			}
			semanticRevision := snapshot.Interception.SemanticRevision
			if semanticRevision == "" {
				semanticRevision = hostresources.Revision("solovey-ui/core-interception-semantics-unavailable/v1")
			}
			mechanism := snapshot.Interception.OriginalDestinationMechanism
			if mechanism == "" {
				mechanism = "UNKNOWN"
			}
			fact, factErr := hostresources.FinalizeInterceptionFactV1(hostresources.InterceptionInboundFactV1{
				ProviderID: p.ProviderID(), ProviderRevision: hostresources.InterceptionProviderRevisionV1,
				ResourceID: resource.ID, EndpointID: endpoint.ID, InboundDatabaseID: snapshot.InboundDatabaseID,
				InboundTag: snapshot.Tag, Kind: kind, Network: endpoint.Key.Network,
				AddressFamily: endpoint.Key.AddressFamily, ConfiguredBind: endpoint.Key.BindAddress,
				ConfiguredPort: endpoint.Key.Port, ObservedEndpointID: observedID(observed),
				Ownership: ownership, ListenerState: listenerState,
				ConfigurationRevision: snapshot.ConfigurationRevision, RuntimeRevision: runtimeRevision,
				RuntimeGenerationRevision: generationRevision, ListenerRevision: listenerRevision,
				CoreSemanticRevision: semanticRevision, LinuxOnly: snapshot.Interception.LinuxOnly,
				TransparentSocketRequired:    snapshot.Interception.TransparentSocketRequired,
				OriginalDestinationMechanism: mechanism,
				OriginalDestinationPreserved: snapshot.Interception.OriginalDestinationPreserved,
				SourcePreserved:              snapshot.Interception.SourcePreserved,
				PolicyRoutingRequired:        snapshot.Interception.PolicyRoutingRequired,
				BoundedUDPFlowState:          snapshot.Interception.BoundedUDPFlowState,
				HealthCapabilityReady:        false, RuntimeReady: runtimeReady,
				LocalOutputCapture: snapshot.Interception.LocalOutputCapture, TUNOwned: snapshot.Interception.TUNOwned,
				ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: reasons,
			})
			if factErr == nil {
				result = append(result, coreInterceptionFactV1{fact: fact, inboundID: snapshot.InboundDatabaseID})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].fact.ResourceID+"\x00"+result[i].fact.EndpointID <
			result[j].fact.ResourceID+"\x00"+result[j].fact.EndpointID
	})
	return result, nil
}

func interceptionNetwork(networks []string, network hostresources.Network) bool {
	for _, value := range networks {
		if strings.EqualFold(value, string(network)) {
			return true
		}
	}
	return false
}

func exactInterceptionSurface(snapshot hostsurface.Snapshot, resourceID string, endpoint hostresources.PublicEndpoint, now time.Time) (
	*hostsurface.HostSurfaceFactV1, hostresources.InterceptionListenerStateV1, hostresources.InterceptionOwnershipV1, []string,
) {
	if snapshot.Truncated {
		return nil, hostresources.InterceptionListenerUnknownV1, hostresources.InterceptionOwnershipUnknownV1,
			[]string{"LISTENER_INVENTORY_TRUNCATED"}
	}
	matches := make([]hostsurface.HostSurfaceFactV1, 0, 1)
	for _, fact := range snapshot.Facts {
		if fact.RegisteredResourceID == resourceID && string(fact.Network) == string(endpoint.Key.Network) &&
			string(fact.Family) == string(endpoint.Key.AddressFamily) &&
			fact.Bind == endpoint.Key.BindAddress && fact.Port == endpoint.Key.Port {
			matches = append(matches, fact)
		}
	}
	if len(matches) == 0 {
		return nil, hostresources.InterceptionListenerUnobservedV1, hostresources.InterceptionOwnershipUnknownV1,
			[]string{"LISTENER_UNOBSERVED"}
	}
	if len(matches) != 1 {
		return nil, hostresources.InterceptionListenerUnknownV1, hostresources.InterceptionOwnershipUnknownV1,
			[]string{"LISTENER_OBSERVATION_AMBIGUOUS"}
	}
	value := matches[0]
	if value.IsStale(now) {
		return &value, hostresources.InterceptionListenerStaleV1, hostresources.InterceptionOwnershipUnknownV1,
			[]string{"LISTENER_OBSERVATION_STALE"}
	}
	if value.Classification == hostsurface.ClassificationManagedExact && value.ListenerOwner != nil && value.ListenerOwner.Valid(now) {
		return &value, hostresources.InterceptionListenerObservedExactV1, hostresources.InterceptionProviderManagedV1, nil
	}
	if value.Classification == hostsurface.ClassificationExpectedExternal || value.Classification == hostsurface.ClassificationForeign {
		return &value, hostresources.InterceptionListenerForeignV1, hostresources.InterceptionExternalManagedV1,
			[]string{"LISTENER_EXTERNAL_MANAGED"}
	}
	return &value, hostresources.InterceptionListenerUnknownV1, hostresources.InterceptionOwnershipUnknownV1,
		[]string{"LISTENER_OWNER_UNPROVEN"}
}

func observedID(value *hostsurface.HostSurfaceFactV1) string {
	if value == nil {
		return ""
	}
	return value.ID
}

func observedRevision(value *hostsurface.HostSurfaceFactV1) string {
	if value == nil {
		return "unobserved"
	}
	return hostresources.Revision(struct {
		Schema, ID, Configuration, Classification string
		Cookie                                    uint64
	}{"solovey-ui/core-interception-observed-listener/v1", value.ID, value.ConfigurationRevision, string(value.Classification), value.SocketCookie})
}

func (p *CoreInterceptionProviderV1) AcquireInterceptionLease(ctx context.Context, request hostresources.AcquireInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.InterceptionLeaseV1{}, errors.New("interception_lease_acquire_request_v1_invalid")
	}
	now := p.currentTime()
	facts, err := p.interceptionFacts(ctx, now)
	if err != nil {
		return hostresources.InterceptionLeaseV1{}, err
	}
	inboundID := uint(0)
	for _, value := range facts {
		if hostresources.ResolveExactInterceptionV1(request.ExactReference, value.fact, now) == nil {
			inboundID = value.inboundID
			break
		}
	}
	if inboundID == 0 {
		return hostresources.InterceptionLeaseV1{}, errors.New("interception_reference_v1_stale")
	}
	leaseID := "core-interception-" + hostresources.Revision(struct{ Holder, Reference string }{
		request.HolderID, request.ExactReference.CanonicalReferenceRevision,
	})[:32]
	var result hostresources.InterceptionLeaseV1
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.InboundEndpointLease
		if queryErr := tx.Where("provider_id = ? AND resource_id = ? AND endpoint_id = ? AND state <> ?",
			p.ProviderID(), request.ExactReference.ResourceID, request.ExactReference.EndpointID,
			string(hostresources.EndpointLeaseReleased)).Find(&rows).Error; queryErr != nil {
			return queryErr
		}
		if len(rows) > 1 {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		if len(rows) == 1 {
			current, decodeErr := decodeCoreInterceptionLeaseV1(rows[0])
			if decodeErr == nil && current.LeaseID == leaseID && current.HolderID == request.HolderID &&
				current.ExactReference.CanonicalReferenceRevision == request.ExactReference.CanonicalReferenceRevision &&
				current.ExpiresAt > now.Unix() {
				result = current
				return nil
			}
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		result, err = hostresources.FinalizeInterceptionLeaseV1(hostresources.InterceptionLeaseV1{
			LeaseID: leaseID, AuthorityProviderID: p.ProviderID(), HolderID: request.HolderID,
			ExactReference: request.ExactReference, State: hostresources.EndpointLeaseReserved,
			IssuedAt: now.Unix(), RenewedAt: now.Unix(),
			ExpiresAt: now.Add(time.Duration(request.FreshnessSeconds) * time.Second).Unix(),
		})
		if err != nil {
			return err
		}
		row, encodeErr := encodeCoreInterceptionLeaseV1(result, inboundID, request.RequestID)
		if encodeErr != nil {
			return encodeErr
		}
		return tx.Create(&row).Error
	})
	return result, err
}

func (p *CoreInterceptionProviderV1) FenceInterceptionLease(ctx context.Context, request hostresources.MutateInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return p.mutateInterceptionLease(ctx, request, hostresources.EndpointLeaseFence)
}

func (p *CoreInterceptionProviderV1) ActivateInterceptionLease(ctx context.Context, request hostresources.MutateInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return p.mutateInterceptionLease(ctx, request, hostresources.EndpointLeaseActivate)
}

func (p *CoreInterceptionProviderV1) RenewInterceptionLease(ctx context.Context, request hostresources.MutateInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	return p.mutateInterceptionLease(ctx, request, hostresources.EndpointLeaseRenew)
}

func (p *CoreInterceptionProviderV1) mutateInterceptionLease(ctx context.Context, request hostresources.MutateInterceptionLeaseRequestV1, mutation hostresources.EndpointLeaseMutation) (hostresources.InterceptionLeaseV1, error) {
	if request.Validate(mutation == hostresources.EndpointLeaseRenew) != nil {
		return hostresources.InterceptionLeaseV1{}, errors.New("interception_lease_mutation_request_v1_invalid")
	}
	now := p.currentTime()
	var result hostresources.InterceptionLeaseV1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, current, err := loadCoreInterceptionLeaseV1(tx, request.LeaseID)
		if err != nil {
			return err
		}
		if row.LastRequestID == request.RequestID {
			result = current
			return nil
		}
		if current.LeaseRevision != request.ExpectedRevision {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		next := current
		switch mutation {
		case hostresources.EndpointLeaseFence:
			next.State = hostresources.EndpointLeaseMutationPending
		case hostresources.EndpointLeaseActivate:
			next.State = hostresources.EndpointLeaseActive
		case hostresources.EndpointLeaseRenew:
			next.State = hostresources.EndpointLeaseActive
			next.RenewedAt = now.Unix()
			next.ExpiresAt = now.Add(time.Duration(request.FreshnessSeconds) * time.Second).Unix()
		default:
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		next, err = hostresources.FinalizeInterceptionLeaseV1(next)
		if err != nil || hostresources.ValidateInterceptionLeaseTransitionV1(current, next, request.ExpectedRevision, mutation, now) != nil {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		encoded, err := encodeCoreInterceptionLeaseV1(next, row.InboundID, request.RequestID)
		if err != nil {
			return err
		}
		update := tx.Model(&model.InboundEndpointLease{}).
			Where("lease_id = ? AND lease_revision = ?", row.LeaseID, current.LeaseRevision).Updates(encoded)
		if update.Error != nil || update.RowsAffected != 1 {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		result = next
		return nil
	})
	return result, err
}

func (p *CoreInterceptionProviderV1) ReleaseInterceptionLease(ctx context.Context, request hostresources.ReleaseInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.InterceptionLeaseV1{}, errors.New("interception_lease_release_request_v1_invalid")
	}
	now := p.currentTime()
	var result hostresources.InterceptionLeaseV1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, current, err := loadCoreInterceptionLeaseV1(tx, request.LeaseID)
		if err != nil {
			return err
		}
		if row.LastRequestID == request.RequestID {
			result = current
			return nil
		}
		if current.LeaseRevision != request.ExpectedRevision {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		next := current
		next.State, next.ReleasedAt = hostresources.EndpointLeaseReleased, now.Unix()
		next, err = hostresources.FinalizeInterceptionLeaseV1(next)
		if err != nil || hostresources.ValidateInterceptionLeaseTransitionV1(current, next, request.ExpectedRevision, hostresources.EndpointLeaseRelease, now) != nil {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		encoded, err := encodeCoreInterceptionLeaseV1(next, row.InboundID, request.RequestID)
		if err != nil {
			return err
		}
		update := tx.Model(&model.InboundEndpointLease{}).
			Where("lease_id = ? AND lease_revision = ?", row.LeaseID, current.LeaseRevision).Updates(encoded)
		if update.Error != nil || update.RowsAffected != 1 {
			return hostresources.ErrInterceptionLeaseConflictV1
		}
		result = next
		return nil
	})
	return result, err
}

func (p *CoreInterceptionProviderV1) GetInterceptionLease(ctx context.Context, request hostresources.GetInterceptionLeaseRequestV1) (hostresources.InterceptionLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.InterceptionLeaseV1{}, errors.New("interception_lease_get_request_v1_invalid")
	}
	_, result, err := loadCoreInterceptionLeaseV1(p.db.WithContext(ctx), request.LeaseID)
	return result, err
}

func (p *CoreInterceptionProviderV1) ListInterceptionLeases(ctx context.Context, request hostresources.ListInterceptionLeasesRequestV1) ([]hostresources.InterceptionLeaseV1, error) {
	if request.Validate() != nil {
		return nil, errors.New("interception_lease_list_request_v1_invalid")
	}
	var rows []model.InboundEndpointLease
	if err := p.db.WithContext(ctx).Where("provider_id = ? AND holder_id = ? AND lease_id LIKE ?",
		p.ProviderID(), request.HolderID, "core-interception-%").Order("lease_id ASC").Limit(int(request.Limit)).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]hostresources.InterceptionLeaseV1, 0, len(rows))
	for _, row := range rows {
		lease, err := decodeCoreInterceptionLeaseV1(row)
		if err != nil {
			return nil, err
		}
		result = append(result, lease)
	}
	return result, nil
}

func (p *CoreInterceptionProviderV1) currentTime() time.Time {
	if p != nil && p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func loadCoreInterceptionLeaseV1(db *gorm.DB, leaseID string) (model.InboundEndpointLease, hostresources.InterceptionLeaseV1, error) {
	var row model.InboundEndpointLease
	if err := db.Where("lease_id = ?", leaseID).First(&row).Error; err != nil {
		return row, hostresources.InterceptionLeaseV1{}, err
	}
	lease, err := decodeCoreInterceptionLeaseV1(row)
	return row, lease, err
}

func decodeCoreInterceptionLeaseV1(row model.InboundEndpointLease) (hostresources.InterceptionLeaseV1, error) {
	var lease hostresources.InterceptionLeaseV1
	var reference hostresources.InterceptionReferenceV1
	if json.Unmarshal(row.LeaseJSON, &lease) != nil || json.Unmarshal(row.ExactReferenceJSON, &reference) != nil ||
		lease.Validate() != nil || lease.ExactReference.CanonicalReferenceRevision != reference.CanonicalReferenceRevision ||
		row.LeaseID != lease.LeaseID || row.ProviderID != lease.AuthorityProviderID || row.HolderID != lease.HolderID ||
		row.ResourceID != lease.ExactReference.ResourceID || row.EndpointID != lease.ExactReference.EndpointID ||
		row.LeaseRevision != lease.LeaseRevision || row.State != string(lease.State) ||
		row.IssuedAtUnix != lease.IssuedAt || row.RenewedAtUnix != lease.RenewedAt ||
		row.ExpiresAtUnix != lease.ExpiresAt || row.ReleasedAtUnix != lease.ReleasedAt {
		return hostresources.InterceptionLeaseV1{}, errors.New("interception_lease_authority_v1_invalid")
	}
	return lease, nil
}

func encodeCoreInterceptionLeaseV1(lease hostresources.InterceptionLeaseV1, inboundID uint, requestID string) (model.InboundEndpointLease, error) {
	if inboundID == 0 || lease.Validate() != nil || strings.TrimSpace(requestID) == "" {
		return model.InboundEndpointLease{}, errors.New("interception_lease_authority_v1_invalid")
	}
	referenceJSON, err := json.Marshal(lease.ExactReference)
	if err != nil {
		return model.InboundEndpointLease{}, err
	}
	leaseJSON, err := json.Marshal(lease)
	if err != nil {
		return model.InboundEndpointLease{}, err
	}
	return model.InboundEndpointLease{
		LeaseID: lease.LeaseID, InboundID: inboundID, ProviderID: lease.AuthorityProviderID, HolderID: lease.HolderID,
		ResourceID: lease.ExactReference.ResourceID, EndpointID: lease.ExactReference.EndpointID,
		ExactReferenceJSON: referenceJSON, LeaseJSON: leaseJSON, LeaseRevision: lease.LeaseRevision,
		State: string(lease.State), LastRequestID: requestID, IssuedAtUnix: lease.IssuedAt,
		RenewedAtUnix: lease.RenewedAt, ExpiresAtUnix: lease.ExpiresAt, ReleasedAtUnix: lease.ReleasedAt,
	}, nil
}

var _ hostresources.InterceptionProviderV1 = (*CoreInterceptionProviderV1)(nil)
