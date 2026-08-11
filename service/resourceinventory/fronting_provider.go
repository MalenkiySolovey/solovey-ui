package resourceinventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/gorm"
)

const coreFrontingBackendProviderIDV1 = "core"

type CoreFrontingBackendProviderV1 struct {
	db        *gorm.DB
	now       func() time.Time
	snapshots func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error)
}

func NewCoreFrontingBackendProviderV1(db *gorm.DB, control *coreinboundcontrol.Service) *CoreFrontingBackendProviderV1 {
	provider := &CoreFrontingBackendProviderV1{db: db, now: time.Now}
	if control != nil {
		provider.snapshots = control.ListSnapshots
	}
	return provider
}

func (p *CoreFrontingBackendProviderV1) ProviderID() string { return coreFrontingBackendProviderIDV1 }

type coreFrontingFactV1 struct {
	fact      hostresources.FrontingBackendFactV1
	inboundID uint
}

func (p *CoreFrontingBackendProviderV1) FrontingBackendFactsV1(ctx context.Context, now time.Time) ([]hostresources.FrontingBackendFactV1, error) {
	values, err := p.frontingFacts(ctx, now)
	if err != nil {
		return nil, err
	}
	result := make([]hostresources.FrontingBackendFactV1, 0, len(values))
	for _, value := range values {
		result = append(result, value.fact)
	}
	return result, nil
}

func (p *CoreFrontingBackendProviderV1) frontingFacts(ctx context.Context, now time.Time) ([]coreFrontingFactV1, error) {
	if p == nil || p.db == nil {
		return nil, errors.New("core_fronting_backend_provider_unavailable")
	}
	if p.snapshots == nil {
		return nil, errors.New("core_fronting_backend_control_unavailable")
	}
	now = now.UTC()
	if now.IsZero() {
		now = p.currentTime()
	}
	snapshots, err := p.snapshots(ctx, hostresources.MaxResourceFacts+1)
	if err != nil || len(snapshots) > hostresources.MaxResourceFacts {
		return nil, errors.New("core_fronting_backend_inventory_unavailable")
	}
	result := make([]coreFrontingFactV1, 0, len(snapshots))
	for _, snapshot := range snapshots {
		resource := inboundResourceAt(snapshot, now)
		if resource.Kind != "inbound" || resource.Owner != coreFrontingBackendProviderIDV1 || !resource.Capabilities.Known {
			continue
		}
		for _, endpoint := range resource.Endpoints {
			if endpoint.Key.Network != hostresources.NetworkTCP || endpoint.Intent != hostresources.EndpointIntentLocal {
				continue
			}
			healthReady := snapshot.Effective.RuntimeAvailable && snapshot.Effective.Present && snapshot.Effective.ConfigurationProven &&
				snapshot.Effective.Revision != "" && len(snapshot.Effective.ReasonCodes) == 0
			reasons := []string(nil)
			if !healthReady {
				reasons = []string{"core_backend_health_unavailable"}
			}
			fact, factErr := hostresources.NewFrontingBackendFactV1(hostresources.FrontingBackendFactV1{
				ProviderID: coreFrontingBackendProviderIDV1, ContributorID: "core", ProviderRevision: "core-fronting-backend-v1",
				HealthRevision: hostresources.Revision(struct {
					Schema, EffectiveRevision, ConfigurationRevision string
					RuntimeAvailable, Present, ConfigurationProven   bool
				}{"solovey-ui/core-fronting-health/v1", snapshot.Effective.Revision, snapshot.ConfigurationRevision,
					snapshot.Effective.RuntimeAvailable, snapshot.Effective.Present, snapshot.Effective.ConfigurationProven}),
				CapacityRevision: hostresources.Revision("solovey-ui/core-fronting-capacity-unbounded/v1"),
				Ownership:        hostresources.FrontingBackendProviderManaged, AcceptsProxyProtocol: endpoint.ProxyProtocol,
				CanReachManagement: hostresources.CapabilityNo, HealthReady: healthReady, CapacityReady: true,
				ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: reasons,
			}, resource, endpoint)
			if factErr == nil {
				result = append(result, coreFrontingFactV1{fact: fact, inboundID: snapshot.InboundDatabaseID})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i].fact.ResourceID + "\x00" + result[i].fact.EndpointID
		right := result[j].fact.ResourceID + "\x00" + result[j].fact.EndpointID
		return left < right
	})
	return result, nil
}

func (p *CoreFrontingBackendProviderV1) AcquireEndpointLease(ctx context.Context, request hostresources.AcquireEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.EndpointLeaseV1{}, errors.New("endpoint_lease_acquire_request_v1_invalid")
	}
	now := p.currentTime()
	facts, err := p.frontingFacts(ctx, now)
	if err != nil {
		return hostresources.EndpointLeaseV1{}, err
	}
	inboundID := uint(0)
	for _, value := range facts {
		if hostresources.ResolveExactFrontingBackendV1(request.ExactReference, value.fact, now) == nil {
			inboundID = value.inboundID
			break
		}
	}
	if inboundID == 0 {
		return hostresources.EndpointLeaseV1{}, errors.New("fronting_backend_reference_v1_stale")
	}
	leaseID := "core-endpoint-" + hostresources.Revision(struct{ Holder, Reference string }{request.HolderID, request.ExactReference.CanonicalReferenceRevision})[:32]
	var result hostresources.EndpointLeaseV1
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.InboundEndpointLease
		if queryErr := tx.Where("provider_id = ? AND resource_id = ? AND endpoint_id = ? AND state <> ?", p.ProviderID(), request.ExactReference.ResourceID,
			request.ExactReference.EndpointID, string(hostresources.EndpointLeaseReleased)).Find(&rows).Error; queryErr != nil {
			return queryErr
		}
		if len(rows) > 1 {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		if len(rows) == 1 {
			current, decodeErr := decodeCoreEndpointLeaseV1(rows[0])
			if decodeErr != nil {
				return decodeErr
			}
			if current.LeaseID == leaseID && current.HolderID == request.HolderID && current.ExactReference == request.ExactReference && current.ExpiresAt > now.Unix() {
				result = current
				return nil
			}
			return hostresources.ErrEndpointLeaseConflictV1
		}
		result, err = hostresources.FinalizeEndpointLeaseV1(hostresources.EndpointLeaseV1{
			LeaseID: leaseID, AuthorityProviderID: p.ProviderID(), HolderID: request.HolderID, ExactReference: request.ExactReference,
			State: hostresources.EndpointLeaseReserved, IssuedAt: now.Unix(), RenewedAt: now.Unix(),
			ExpiresAt: now.Add(time.Duration(request.FreshnessSeconds) * time.Second).Unix(), ReasonCodes: []string{},
		})
		if err != nil {
			return err
		}
		row, rowErr := encodeCoreEndpointLeaseV1(result, inboundID, request.RequestID)
		if rowErr != nil {
			return rowErr
		}
		return tx.Create(&row).Error
	})
	return result, err
}

func (p *CoreFrontingBackendProviderV1) FenceEndpointLease(ctx context.Context, request hostresources.MutateEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	return p.mutateEndpointLease(ctx, request, hostresources.EndpointLeaseFence)
}

func (p *CoreFrontingBackendProviderV1) ActivateEndpointLease(ctx context.Context, request hostresources.MutateEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	return p.mutateEndpointLease(ctx, request, hostresources.EndpointLeaseActivate)
}

func (p *CoreFrontingBackendProviderV1) mutateEndpointLease(ctx context.Context, request hostresources.MutateEndpointLeaseRequestV1, mutation hostresources.EndpointLeaseMutation) (hostresources.EndpointLeaseV1, error) {
	if request.Validate(false) != nil {
		return hostresources.EndpointLeaseV1{}, errors.New("endpoint_lease_mutation_request_v1_invalid")
	}
	now := p.currentTime()
	var result hostresources.EndpointLeaseV1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, current, err := loadCoreEndpointLeaseV1(tx, request.LeaseID)
		if err != nil {
			return err
		}
		if row.LastRequestID == request.RequestID {
			result = current
			return nil
		}
		if current.LeaseRevision != request.ExpectedRevision {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		next := current
		switch mutation {
		case hostresources.EndpointLeaseFence:
			next.State = hostresources.EndpointLeaseMutationPending
		case hostresources.EndpointLeaseActivate:
			next.State = hostresources.EndpointLeaseActive
		default:
			return errors.New("endpoint_lease_transition_illegal")
		}
		next, err = hostresources.FinalizeEndpointLeaseV1(next)
		if err != nil || hostresources.ValidateEndpointLeaseTransitionV1(current, next, hostresources.EndpointLeaseCASV1{
			RequestID: request.RequestID, LeaseID: request.LeaseID, ExpectedRevision: request.ExpectedRevision,
		}, mutation, now) != nil {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		encoded, err := encodeCoreEndpointLeaseV1(next, row.InboundID, request.RequestID)
		if err != nil {
			return err
		}
		update := tx.Model(&model.InboundEndpointLease{}).Where("lease_id = ? AND lease_revision = ?", row.LeaseID, current.LeaseRevision).Updates(encoded)
		if update.Error != nil || update.RowsAffected != 1 {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		result = next
		return nil
	})
	return result, err
}

func (p *CoreFrontingBackendProviderV1) ReleaseEndpointLease(ctx context.Context, request hostresources.ReleaseEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.EndpointLeaseV1{}, errors.New("endpoint_lease_release_request_v1_invalid")
	}
	now := p.currentTime()
	var result hostresources.EndpointLeaseV1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, current, err := loadCoreEndpointLeaseV1(tx, request.LeaseID)
		if err != nil {
			return err
		}
		if row.LastRequestID == request.RequestID {
			result = current
			return nil
		}
		if current.LeaseRevision != request.ExpectedRevision {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		next := current
		next.State, next.ReleasedAt = hostresources.EndpointLeaseReleased, now.Unix()
		next, err = hostresources.FinalizeEndpointLeaseV1(next)
		if err != nil || hostresources.ValidateEndpointLeaseTransitionV1(current, next, hostresources.EndpointLeaseCASV1{
			RequestID: request.RequestID, LeaseID: request.LeaseID, ExpectedRevision: request.ExpectedRevision,
		}, hostresources.EndpointLeaseRelease, now) != nil {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		encoded, err := encodeCoreEndpointLeaseV1(next, row.InboundID, request.RequestID)
		if err != nil {
			return err
		}
		update := tx.Model(&model.InboundEndpointLease{}).Where("lease_id = ? AND lease_revision = ?", row.LeaseID, current.LeaseRevision).Updates(encoded)
		if update.Error != nil || update.RowsAffected != 1 {
			return hostresources.ErrEndpointLeaseConflictV1
		}
		result = next
		return nil
	})
	return result, err
}

func (p *CoreFrontingBackendProviderV1) GetEndpointLease(ctx context.Context, request hostresources.GetEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	if request.Validate() != nil {
		return hostresources.EndpointLeaseV1{}, errors.New("endpoint_lease_get_request_v1_invalid")
	}
	_, lease, err := loadCoreEndpointLeaseV1(p.db.WithContext(ctx), request.LeaseID)
	return lease, err
}

func (p *CoreFrontingBackendProviderV1) ListEndpointLeases(ctx context.Context, request hostresources.ListEndpointLeasesRequestV1) ([]hostresources.EndpointLeaseV1, error) {
	if request.Validate() != nil {
		return nil, errors.New("endpoint_lease_list_request_v1_invalid")
	}
	var rows []model.InboundEndpointLease
	if err := p.db.WithContext(ctx).Where("provider_id = ? AND holder_id = ?", p.ProviderID(), request.HolderID).Order("lease_id ASC").Limit(int(request.Limit)).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]hostresources.EndpointLeaseV1, 0, len(rows))
	for _, row := range rows {
		lease, err := decodeCoreEndpointLeaseV1(row)
		if err != nil {
			return nil, err
		}
		result = append(result, lease)
	}
	return result, nil
}

func (p *CoreFrontingBackendProviderV1) currentTime() time.Time {
	if p != nil && p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func loadCoreEndpointLeaseV1(db *gorm.DB, leaseID string) (model.InboundEndpointLease, hostresources.EndpointLeaseV1, error) {
	var row model.InboundEndpointLease
	if err := db.Where("lease_id = ?", leaseID).First(&row).Error; err != nil {
		return row, hostresources.EndpointLeaseV1{}, err
	}
	lease, err := decodeCoreEndpointLeaseV1(row)
	return row, lease, err
}

func decodeCoreEndpointLeaseV1(row model.InboundEndpointLease) (hostresources.EndpointLeaseV1, error) {
	var lease hostresources.EndpointLeaseV1
	var reference hostresources.FrontingBackendReferenceV1
	if json.Unmarshal(row.LeaseJSON, &lease) != nil || json.Unmarshal(row.ExactReferenceJSON, &reference) != nil || lease.Validate() != nil ||
		lease.ExactReference != reference || row.LeaseID != lease.LeaseID || row.ProviderID != lease.AuthorityProviderID || row.HolderID != lease.HolderID ||
		row.ResourceID != lease.ExactReference.ResourceID || row.EndpointID != lease.ExactReference.EndpointID || row.LeaseRevision != lease.LeaseRevision ||
		row.State != string(lease.State) || row.IssuedAtUnix != lease.IssuedAt || row.RenewedAtUnix != lease.RenewedAt || row.ExpiresAtUnix != lease.ExpiresAt ||
		row.ReleasedAtUnix != lease.ReleasedAt {
		return hostresources.EndpointLeaseV1{}, errors.New("endpoint_lease_authority_v1_invalid")
	}
	return lease, nil
}

func encodeCoreEndpointLeaseV1(lease hostresources.EndpointLeaseV1, inboundID uint, requestID string) (model.InboundEndpointLease, error) {
	if inboundID == 0 || lease.Validate() != nil || requestID == "" {
		return model.InboundEndpointLease{}, errors.New("endpoint_lease_authority_v1_invalid")
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
		ExactReferenceJSON: referenceJSON, LeaseJSON: leaseJSON, LeaseRevision: lease.LeaseRevision, State: string(lease.State), LastRequestID: requestID,
		IssuedAtUnix: lease.IssuedAt, RenewedAtUnix: lease.RenewedAt, ExpiresAtUnix: lease.ExpiresAt, ReleasedAtUnix: lease.ReleasedAt,
	}, nil
}

func (p *CoreFrontingBackendProviderV1) String() string {
	return fmt.Sprintf("%s-fronting-backend-provider", p.ProviderID())
}

var _ hostresources.FrontingBackendProviderV1 = (*CoreFrontingBackendProviderV1)(nil)
