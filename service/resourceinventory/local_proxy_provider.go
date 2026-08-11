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

const coreLocalProxyProviderIDV1 = "core"

type CoreLocalProxyProviderV1 struct {
	db        *gorm.DB
	now       func() time.Time
	snapshots func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error)
	surfaces  func() hostsurface.Snapshot
	inventory func(context.Context) hostresources.ResourceSnapshot
}

func NewCoreLocalProxyProviderV1(db *gorm.DB, control *coreinboundcontrol.Service) *CoreLocalProxyProviderV1 {
	provider := &CoreLocalProxyProviderV1{
		db: db, now: time.Now, surfaces: hostsurface.CurrentSnapshot, inventory: hostresources.Snapshot,
	}
	if control != nil {
		provider.snapshots = control.ListSnapshots
	}
	return provider
}

func (*CoreLocalProxyProviderV1) ProviderID() string { return coreLocalProxyProviderIDV1 }

type coreLocalProxyFactV1 struct {
	fact      hostresources.LocalProxyFactV1
	inboundID uint
}

func (p *CoreLocalProxyProviderV1) LocalProxyFactsV1(ctx context.Context, now time.Time) ([]hostresources.LocalProxyFactV1, error) {
	values, err := p.localProxyFacts(ctx, now)
	if err != nil {
		return nil, err
	}
	result := make([]hostresources.LocalProxyFactV1, 0, len(values))
	for _, value := range values {
		result = append(result, value.fact)
	}
	return result, nil
}

func (p *CoreLocalProxyProviderV1) localProxyFacts(ctx context.Context, now time.Time) ([]coreLocalProxyFactV1, error) {
	if p == nil || p.db == nil || p.snapshots == nil {
		return nil, errors.New("core_local_proxy_provider_unavailable")
	}
	now = now.UTC()
	if now.IsZero() {
		now = p.currentTime()
	}
	snapshots, err := p.snapshots(ctx, hostresources.MaxResourceFacts+1)
	if err != nil || len(snapshots) > hostresources.MaxResourceFacts {
		return nil, errors.New("core_local_proxy_inventory_unavailable")
	}
	surfaceSnapshot := hostsurface.Snapshot{}
	if p.surfaces != nil {
		surfaceSnapshot = p.surfaces()
	}
	resourceSnapshot := hostresources.ResourceSnapshot{}
	if p.inventory != nil {
		resourceSnapshot = p.inventory(ctx)
	}
	result := make([]coreLocalProxyFactV1, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !snapshot.LocalProxy.Candidate || (snapshot.Type != "socks" && snapshot.Type != "http" && snapshot.Type != "mixed") {
			continue
		}
		resource := inboundResourceAt(snapshot, now)
		if resource.Kind != "inbound" || resource.Owner != p.ProviderID() {
			continue
		}
		for _, endpoint := range resource.Endpoints {
			if endpoint.Key.Network != hostresources.NetworkTCP {
				continue
			}
			observed, observationReasons := exactLocalProxySurface(surfaceSnapshot, resource.ID, endpoint, now)
			ownership := hostresources.LocalProxyOwnershipUnknown
			if observed != nil {
				switch observed.Classification {
				case hostsurface.ClassificationManagedExact:
					ownership = hostresources.LocalProxyProviderManaged
				case hostsurface.ClassificationExpectedExternal, hostsurface.ClassificationForeign:
					ownership = hostresources.LocalProxyExternalManaged
				}
			}
			management, recovery, exclusionRevision, recoveryRevision, exclusionReasons := localProxyExclusions(resourceSnapshot, resource.ID, endpoint)
			reasons := append(observationReasons, exclusionReasons...)
			runtimeReady := snapshot.Effective.RuntimeAvailable && snapshot.Effective.Present &&
				snapshot.Effective.ConfigurationProven && len(snapshot.Effective.ReasonCodes) == 0
			if !runtimeReady {
				reasons = append(reasons, "RUNTIME_CONFIGURATION_UNPROVEN")
			}
			if snapshot.LocalProxy.SystemProxyEnabled {
				reasons = append(reasons, "SYSTEM_PROXY_ENABLED_NOT_SHIPPED")
			}
			protocols := make([]hostresources.LocalProxyProtocolV1, 0, len(snapshot.LocalProxy.Protocols))
			for _, protocol := range snapshot.LocalProxy.Protocols {
				protocols = append(protocols, hostresources.LocalProxyProtocolV1(protocol))
			}
			authentication := hostresources.LocalProxyAuthenticationUnknown
			if snapshot.LocalProxy.Authentication.Known {
				authentication = hostresources.LocalProxyAuthenticationAbsent
				if snapshot.LocalProxy.Authentication.Expected {
					authentication = hostresources.LocalProxyAuthenticationPresent
				}
			}
			tlsState := hostresources.LocalProxyTLSUnknown
			if snapshot.LocalProxy.Candidate {
				tlsState = hostresources.LocalProxyTLSDisabled
				if snapshot.LocalProxy.TLS.Enabled {
					tlsState = hostresources.LocalProxyTLSEnabled
				}
			}
			systemProxy := hostresources.LocalProxySystemProxyUnknown
			if snapshot.LocalProxy.SystemProxyKnown {
				systemProxy = hostresources.LocalProxySystemProxyDisabled
				if snapshot.LocalProxy.SystemProxyEnabled {
					systemProxy = hostresources.LocalProxySystemProxyEnabled
				}
			}
			healthRevision := hostresources.Revision(struct {
				Schema, Runtime, Configuration, Protocol string
				Ready                                    bool
			}{"solovey-ui/core-local-proxy-health-capability/v1", snapshot.Effective.Revision,
				snapshot.ConfigurationRevision, snapshot.LocalProxy.ProtocolRevision, runtimeReady})
			fact, factErr := hostresources.NewLocalProxyFactV1(hostresources.LocalProxyFactV1{
				ProviderID: p.ProviderID(), ContributorID: "core", InboundDatabaseID: snapshot.InboundDatabaseID,
				InboundType: snapshot.Type, ConfigurationRevision: snapshot.ConfigurationRevision,
				EffectiveRuntimeRevision: snapshot.Effective.Revision, RuntimeIdentityRevision: snapshot.RuntimeIdentityRevision,
				ProviderRevision:   hostresources.LocalProxyProviderRevisionV1,
				CapabilityRevision: hostresources.LocalProxyCapabilityRevisionV1, OwnerRevision: resource.Capabilities.OwnerRevision,
				HealthRevision: healthRevision, CapacityRevision: hostresources.Revision("solovey-ui/core-local-proxy-capacity-bounded/v1"),
				ManagementExclusionRevision: exclusionRevision, RecoveryPathRevision: recoveryRevision,
				Ownership: ownership, Protocols: protocols, Authentication: authentication,
				AuthenticationCount:    snapshot.LocalProxy.Authentication.Count,
				AuthenticationRevision: snapshot.LocalProxy.Authentication.Revision,
				TLS:                    tlsState, TLSRevision: snapshot.LocalProxy.TLSRevision,
				SystemProxy: systemProxy, SystemProxyRevision: snapshot.LocalProxy.SystemProxyRevision,
				DependentUDPAssociation: snapshot.LocalProxy.DependentUDPAssociation,
				StaticUDPListener:       snapshot.LocalProxy.StaticUDPListener, RuntimeReady: runtimeReady,
				HealthCapabilityReady: runtimeReady, CapacityReady: true,
				ManagementCollision: management, RecoveryPathCollision: recovery,
				ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: reasons,
			}, resource, endpoint, observed)
			if factErr == nil {
				result = append(result, coreLocalProxyFactV1{fact: fact, inboundID: snapshot.InboundDatabaseID})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].fact.ResourceID+"\x00"+result[i].fact.EndpointID <
			result[j].fact.ResourceID+"\x00"+result[j].fact.EndpointID
	})
	return result, nil
}

func exactLocalProxySurface(snapshot hostsurface.Snapshot, resourceID string, endpoint hostresources.PublicEndpoint, now time.Time) (*hostsurface.HostSurfaceFactV1, []string) {
	if snapshot.Truncated {
		return nil, []string{"LISTENER_INVENTORY_TRUNCATED"}
	}
	matches := make([]hostsurface.HostSurfaceFactV1, 0, 1)
	for _, fact := range snapshot.Facts {
		if fact.RegisteredResourceID == resourceID && fact.Network == hostsurface.NetworkTCP &&
			fact.Bind == endpoint.Key.BindAddress && fact.Port == endpoint.Key.Port {
			matches = append(matches, fact)
		}
	}
	if len(matches) == 0 {
		return nil, []string{"LISTENER_UNOBSERVED"}
	}
	if len(matches) != 1 {
		return nil, []string{"LISTENER_OBSERVATION_AMBIGUOUS"}
	}
	value := matches[0]
	if value.IsStale(now) {
		return &value, []string{"LISTENER_OBSERVATION_STALE"}
	}
	if value.Classification != hostsurface.ClassificationManagedExact || value.ListenerOwner == nil ||
		!value.ListenerOwner.Valid(now) {
		return &value, []string{"LISTENER_OWNER_UNPROVEN"}
	}
	return &value, nil
}

func localProxyExclusions(snapshot hostresources.ResourceSnapshot, resourceID string, endpoint hostresources.PublicEndpoint) (hostresources.CapabilityValue, hostresources.CapabilityValue, string, string, []string) {
	type exclusionItem struct {
		ResourceID string
		EndpointID string
		Collision  bool
	}
	items := make([]exclusionItem, 0)
	reasons := []string(nil)
	if len(snapshot.Errors) != 0 {
		reasons = append(reasons, "MANAGEMENT_INVENTORY_UNAVAILABLE")
		return hostresources.CapabilityUnknown, hostresources.CapabilityUnknown,
			hostresources.Revision(struct {
				Schema string
				Bad    bool
			}{"solovey-ui/local-proxy-management-exclusion/v1", true}),
			hostresources.Revision(struct {
				Schema string
				Bad    bool
			}{"solovey-ui/local-proxy-recovery-exclusion/v1", true}), reasons
	}
	collision := false
	for _, resource := range snapshot.Resources {
		if resource.ID == resourceID || (resource.Kind != "panel" && resource.Kind != "subscription" && resource.Kind != "management") {
			continue
		}
		for _, candidate := range resource.Endpoints {
			same := candidate.Key.Network == endpoint.Key.Network && candidate.Key.AddressFamily == endpoint.Key.AddressFamily &&
				candidate.Key.BindAddress == endpoint.Key.BindAddress && candidate.Key.Port == endpoint.Key.Port
			items = append(items, exclusionItem{ResourceID: resource.ID, EndpointID: candidate.ID, Collision: same})
			collision = collision || same
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ResourceID+"\x00"+items[i].EndpointID < items[j].ResourceID+"\x00"+items[j].EndpointID
	})
	management := hostresources.CapabilityNo
	recovery := hostresources.CapabilityNo
	if collision {
		management, recovery = hostresources.CapabilityYes, hostresources.CapabilityYes
		reasons = append(reasons, "MANAGEMENT_LISTENER_COLLISION")
	}
	return management, recovery,
		hostresources.Revision(struct {
			Schema string
			Items  []exclusionItem
		}{"solovey-ui/local-proxy-management-exclusion/v1", items}),
		hostresources.Revision(struct {
			Schema string
			Items  []exclusionItem
		}{"solovey-ui/local-proxy-recovery-exclusion/v1", items}), reasons
}

func (p *CoreLocalProxyProviderV1) AcquireLocalProxyGuardLease(ctx context.Context, request hostresources.AcquireLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, errors.New("local_proxy_guard_lease_acquire_request_v1_invalid")
	}
	now := p.currentTime()
	facts, err := p.localProxyFacts(ctx, now)
	if err != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, err
	}
	inboundID := uint(0)
	for _, value := range facts {
		if hostresources.ResolveExactLocalProxyV1(request.ExactReference, value.fact, now) == nil {
			inboundID = value.inboundID
			break
		}
	}
	if inboundID == 0 {
		return hostresources.LocalProxyGuardLeaseV1{}, errors.New("local_proxy_reference_v1_stale")
	}
	leaseID := "core-local-proxy-" + hostresources.Revision(struct{ Holder, Reference string }{
		request.HolderID, request.ExactReference.CanonicalReferenceRevision,
	})[:32]
	var result hostresources.LocalProxyGuardLeaseV1
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.InboundEndpointLease
		if queryErr := tx.Where("provider_id = ? AND resource_id = ? AND endpoint_id = ? AND state <> ?",
			p.ProviderID(), request.ExactReference.ResourceID, request.ExactReference.EndpointID,
			string(hostresources.EndpointLeaseReleased)).Find(&rows).Error; queryErr != nil {
			return queryErr
		}
		if len(rows) > 1 {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		if len(rows) == 1 {
			current, decodeErr := decodeCoreLocalProxyGuardLeaseV1(rows[0])
			if decodeErr == nil && current.LeaseID == leaseID && current.HolderID == request.HolderID &&
				current.ExactReference.CanonicalReferenceRevision == request.ExactReference.CanonicalReferenceRevision &&
				current.ExpiresAt > now.Unix() {
				result = current
				return nil
			}
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		result, err = hostresources.FinalizeLocalProxyGuardLeaseV1(hostresources.LocalProxyGuardLeaseV1{
			LeaseID: leaseID, AuthorityProviderID: p.ProviderID(), HolderID: request.HolderID,
			ExactReference: request.ExactReference, State: hostresources.EndpointLeaseReserved,
			IssuedAt: now.Unix(), RenewedAt: now.Unix(),
			ExpiresAt: now.Add(time.Duration(request.FreshnessSeconds) * time.Second).Unix(), ReasonCodes: []string{},
		})
		if err != nil {
			return err
		}
		row, encodeErr := encodeCoreLocalProxyGuardLeaseV1(result, inboundID, request.RequestID)
		if encodeErr != nil {
			return encodeErr
		}
		return tx.Create(&row).Error
	})
	return result, err
}

func (p *CoreLocalProxyProviderV1) FenceLocalProxyGuardLease(ctx context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	return p.mutateLocalProxyGuardLease(ctx, request, hostresources.EndpointLeaseFence)
}

func (p *CoreLocalProxyProviderV1) ActivateLocalProxyGuardLease(ctx context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	return p.mutateLocalProxyGuardLease(ctx, request, hostresources.EndpointLeaseActivate)
}

func (p *CoreLocalProxyProviderV1) RenewLocalProxyGuardLease(ctx context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	return p.mutateLocalProxyGuardLease(ctx, request, hostresources.EndpointLeaseRenew)
}

func (p *CoreLocalProxyProviderV1) mutateLocalProxyGuardLease(ctx context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1, mutation hostresources.EndpointLeaseMutation) (hostresources.LocalProxyGuardLeaseV1, error) {
	if request.Validate(mutation == hostresources.EndpointLeaseRenew) != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, errors.New("local_proxy_guard_lease_mutation_request_v1_invalid")
	}
	now := p.currentTime()
	var result hostresources.LocalProxyGuardLeaseV1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, current, err := loadCoreLocalProxyGuardLeaseV1(tx, request.LeaseID)
		if err != nil {
			return err
		}
		if row.LastRequestID == request.RequestID {
			result = current
			return nil
		}
		if current.LeaseRevision != request.ExpectedRevision {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
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
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		next, err = hostresources.FinalizeLocalProxyGuardLeaseV1(next)
		if err != nil || hostresources.ValidateLocalProxyGuardLeaseTransitionV1(current, next, request.ExpectedRevision, mutation, now) != nil {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		encoded, err := encodeCoreLocalProxyGuardLeaseV1(next, row.InboundID, request.RequestID)
		if err != nil {
			return err
		}
		update := tx.Model(&model.InboundEndpointLease{}).
			Where("lease_id = ? AND lease_revision = ?", row.LeaseID, current.LeaseRevision).Updates(encoded)
		if update.Error != nil || update.RowsAffected != 1 {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		result = next
		return nil
	})
	return result, err
}

func (p *CoreLocalProxyProviderV1) ReleaseLocalProxyGuardLease(ctx context.Context, request hostresources.ReleaseLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, errors.New("local_proxy_guard_lease_release_request_v1_invalid")
	}
	now := p.currentTime()
	var result hostresources.LocalProxyGuardLeaseV1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, current, err := loadCoreLocalProxyGuardLeaseV1(tx, request.LeaseID)
		if err != nil {
			return err
		}
		if row.LastRequestID == request.RequestID {
			result = current
			return nil
		}
		if current.LeaseRevision != request.ExpectedRevision {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		next := current
		next.State, next.ReleasedAt = hostresources.EndpointLeaseReleased, now.Unix()
		next, err = hostresources.FinalizeLocalProxyGuardLeaseV1(next)
		if err != nil || hostresources.ValidateLocalProxyGuardLeaseTransitionV1(current, next, request.ExpectedRevision, hostresources.EndpointLeaseRelease, now) != nil {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		encoded, err := encodeCoreLocalProxyGuardLeaseV1(next, row.InboundID, request.RequestID)
		if err != nil {
			return err
		}
		update := tx.Model(&model.InboundEndpointLease{}).
			Where("lease_id = ? AND lease_revision = ?", row.LeaseID, current.LeaseRevision).Updates(encoded)
		if update.Error != nil || update.RowsAffected != 1 {
			return hostresources.ErrLocalProxyGuardLeaseConflictV1
		}
		result = next
		return nil
	})
	return result, err
}

func (p *CoreLocalProxyProviderV1) GetLocalProxyGuardLease(ctx context.Context, request hostresources.GetLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, errors.New("local_proxy_guard_lease_get_request_v1_invalid")
	}
	_, result, err := loadCoreLocalProxyGuardLeaseV1(p.db.WithContext(ctx), request.LeaseID)
	return result, err
}

func (p *CoreLocalProxyProviderV1) ListLocalProxyGuardLeases(ctx context.Context, request hostresources.ListLocalProxyGuardLeasesRequestV1) ([]hostresources.LocalProxyGuardLeaseV1, error) {
	if request.Validate() != nil {
		return nil, errors.New("local_proxy_guard_lease_list_request_v1_invalid")
	}
	var rows []model.InboundEndpointLease
	if err := p.db.WithContext(ctx).Where("provider_id = ? AND holder_id = ? AND lease_id LIKE ?",
		p.ProviderID(), request.HolderID, "core-local-proxy-%").Order("lease_id ASC").Limit(int(request.Limit)).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]hostresources.LocalProxyGuardLeaseV1, 0, len(rows))
	for _, row := range rows {
		lease, err := decodeCoreLocalProxyGuardLeaseV1(row)
		if err != nil {
			return nil, err
		}
		result = append(result, lease)
	}
	return result, nil
}

func (p *CoreLocalProxyProviderV1) currentTime() time.Time {
	if p != nil && p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func loadCoreLocalProxyGuardLeaseV1(db *gorm.DB, leaseID string) (model.InboundEndpointLease, hostresources.LocalProxyGuardLeaseV1, error) {
	var row model.InboundEndpointLease
	if err := db.Where("lease_id = ?", leaseID).First(&row).Error; err != nil {
		return row, hostresources.LocalProxyGuardLeaseV1{}, err
	}
	lease, err := decodeCoreLocalProxyGuardLeaseV1(row)
	return row, lease, err
}

func decodeCoreLocalProxyGuardLeaseV1(row model.InboundEndpointLease) (hostresources.LocalProxyGuardLeaseV1, error) {
	var lease hostresources.LocalProxyGuardLeaseV1
	var reference hostresources.LocalProxyReferenceV1
	if json.Unmarshal(row.LeaseJSON, &lease) != nil || json.Unmarshal(row.ExactReferenceJSON, &reference) != nil ||
		lease.Validate() != nil || lease.ExactReference.CanonicalReferenceRevision != reference.CanonicalReferenceRevision ||
		row.LeaseID != lease.LeaseID || row.ProviderID != lease.AuthorityProviderID || row.HolderID != lease.HolderID ||
		row.ResourceID != lease.ExactReference.ResourceID || row.EndpointID != lease.ExactReference.EndpointID ||
		row.LeaseRevision != lease.LeaseRevision || row.State != string(lease.State) ||
		row.IssuedAtUnix != lease.IssuedAt || row.RenewedAtUnix != lease.RenewedAt ||
		row.ExpiresAtUnix != lease.ExpiresAt || row.ReleasedAtUnix != lease.ReleasedAt {
		return hostresources.LocalProxyGuardLeaseV1{}, errors.New("local_proxy_guard_lease_authority_v1_invalid")
	}
	return lease, nil
}

func encodeCoreLocalProxyGuardLeaseV1(lease hostresources.LocalProxyGuardLeaseV1, inboundID uint, requestID string) (model.InboundEndpointLease, error) {
	if inboundID == 0 || lease.Validate() != nil || strings.TrimSpace(requestID) == "" {
		return model.InboundEndpointLease{}, errors.New("local_proxy_guard_lease_authority_v1_invalid")
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

var _ hostresources.LocalProxyProviderV1 = (*CoreLocalProxyProviderV1)(nil)
