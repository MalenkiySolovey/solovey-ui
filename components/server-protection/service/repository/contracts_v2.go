package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectiondecision "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/decision"
	protectionevents "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ContractFilter struct {
	PageQuery
	Scope, Kind, ResourceID string
}

func (r *Repository) SaveSignalV2(ctx context.Context, signal domain.ProtectionSignalV2) error {
	if err := signal.Validate(time.Now().UTC()); err != nil {
		return err
	}
	payload, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	safeMeta, err := json.Marshal(signal.SafeMeta)
	if err != nil {
		return err
	}
	model := ProtectionSignalV2Model{SignalID: signal.SignalID, Schema: signal.Schema, SourceID: signal.Source.SourceID, SourceClass: signal.Source.SourceClass, Category: string(signal.Category), Kind: signal.Kind, KnownKind: signal.KnownKind, SubjectType: signal.Subject.Type, SubjectValue: signal.Subject.Value, Scope: string(signal.Scope.Scope), TargetResourceID: signal.Scope.TargetResourceID, EndpointID: signal.Scope.EndpointID, Transport: signal.Scope.Transport, ObservationWindowID: signal.Provenance.ObservationWindowID, ObservedAt: signal.ObservedAt.Unix(), ExpiresAt: signal.ExpiresAt.Unix(), ConfidenceBP: signal.ConfidenceBP, PolicyRevision: signal.Provenance.PolicyRevision, SafeMetaBytes: len(safeMeta), ContractJSON: payload}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createSignalIdempotent(tx, model); err != nil {
			return err
		}
		globalLimit, perTargetLimit := contractRetentionLimits(tx)
		return pruneSignalContracts(tx, globalLimit, perTargetLimit, signal.Scope.TargetResourceID)
	})
}

func (r *Repository) SaveDecisionV2(ctx context.Context, decision domain.ProtectionDecisionV2) error {
	if err := decision.Validate(time.Now().UTC()); err != nil {
		return err
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	model := ProtectionDecisionV2Model{DecisionID: decision.DecisionID, Schema: decision.Schema, PolicyRevision: decision.PolicyRevision, SubjectType: decision.Subject.Type, SubjectValue: decision.Subject.Value, Scope: string(decision.Scope.Scope), RequestedIntent: string(decision.RequestedIntent), ResolvedIntent: string(decision.CapabilityResolution.ResolvedIntent), ActionImplemented: decision.CapabilityResolution.Implemented, State: string(decision.State), CreatedAt: decision.CreatedAt.Unix(), ExpiresAt: decision.ExpiresAt.Unix(), ContractJSON: payload}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "decision_id"}}, DoNothing: true}).Create(&model).Error; err != nil {
			return err
		}
		globalLimit, _ := contractRetentionLimits(tx)
		return pruneDecisionContracts(tx, globalLimit)
	})
}

func (r *Repository) ListSignalsV2(ctx context.Context, filter ContractFilter) ([]ProtectionSignalV2Model, int64, error) {
	query, err := r.query(ctx, &ProtectionSignalV2Model{})
	if err != nil {
		return nil, 0, err
	}
	if filter.Scope != "" {
		query = query.Where("scope = ?", filter.Scope)
	}
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.ResourceID != "" {
		query = query.Where("target_resource_id = ?", filter.ResourceID)
	}
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []ProtectionSignalV2Model
	err = query.Order("observed_at DESC, id DESC").Offset(filter.Offset()).Limit(filter.Limit).Find(&items).Error
	return items, total, err
}
func (r *Repository) ListDecisionsV2(ctx context.Context, filter ContractFilter) ([]ProtectionDecisionV2Model, int64, error) {
	query, err := r.query(ctx, &ProtectionDecisionV2Model{})
	if err != nil {
		return nil, 0, err
	}
	if filter.Scope != "" {
		query = query.Where("scope = ?", filter.Scope)
	}
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []ProtectionDecisionV2Model
	err = query.Order("created_at DESC, id DESC").Offset(filter.Offset()).Limit(filter.Limit).Find(&items).Error
	return items, total, err
}

func (r *Repository) SaveLease(ctx context.Context, value neutralfallback.ReferenceLeaseV1) error {
	if err := value.Validate(time.Now().UTC()); err != nil {
		return err
	}
	reasons, _ := json.Marshal(value.ReasonCodes)
	model := FallbackTargetLeaseModel{LeaseID: value.LeaseID, HolderID: value.HolderID, ProviderID: value.ProviderID, TargetID: value.TargetID, PublishRevision: value.PublishRevision, ContentDigest: value.ContentDigest, ApprovedLocalEndpointID: value.ApprovedLocalEndpointID, ProviderHealthRevision: value.ProviderHealthRevision, IssuedAt: value.IssuedAt, RenewedAt: value.RenewedAt, ExpiresAt: value.ExpiresAt, ReleasedAt: value.ReleasedAt, State: value.State, ReasonCodesJSON: reasons}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&FallbackTargetLeaseModel{}).Where("lease_id = ?", value.LeaseID).Count(&existing).Error; err != nil {
			return err
		}
		limit := neutralfallback.MaxReferenceLeases
		if existing == 0 {
			limit--
		}
		if err := pruneLeaseContracts(tx, limit); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "lease_id"}}, DoUpdates: clause.AssignmentColumns([]string{"holder_id", "provider_id", "target_id", "publish_revision", "content_digest", "approved_local_endpoint_id", "provider_health_revision", "issued_at", "renewed_at", "expires_at", "released_at", "state", "reason_codes_json"})}).Create(&model).Error
	})
}
func (r *Repository) LoadLease(ctx context.Context, id string) (neutralfallback.ReferenceLeaseV1, error) {
	var model FallbackTargetLeaseModel
	err := r.db.WithContext(ctx).Where("lease_id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neutralfallback.ReferenceLeaseV1{}, errors.New("fallback_target_lease_missing")
	}
	if err != nil {
		return neutralfallback.ReferenceLeaseV1{}, err
	}
	var reasons []string
	_ = json.Unmarshal(model.ReasonCodesJSON, &reasons)
	value := neutralfallback.ReferenceLeaseV1{Schema: neutralfallback.LeaseSchemaV1, LeaseID: model.LeaseID, HolderID: model.HolderID, ProviderID: model.ProviderID, TargetID: model.TargetID, PublishRevision: model.PublishRevision, ContentDigest: model.ContentDigest, ApprovedLocalEndpointID: model.ApprovedLocalEndpointID, ProviderHealthRevision: model.ProviderHealthRevision, IssuedAt: model.IssuedAt, RenewedAt: model.RenewedAt, ExpiresAt: model.ExpiresAt, ReleasedAt: model.ReleasedAt, State: model.State, ReasonCodes: reasons}
	now := time.Now().UTC()
	if validateErr := value.Validate(now); validateErr != nil {
		return neutralfallback.ReferenceLeaseV1{}, errors.New("fallback_target_lease_invalid")
	}
	if value.State == "ACTIVE" && !value.Fresh(now) {
		value.State = "STALE"
		value.ReasonCodes = appendUniqueReason(value.ReasonCodes, "fallback_target_lease_stale")
	}
	return value, nil
}
func (r *Repository) ListLeases(ctx context.Context, page PageQuery) ([]FallbackTargetLeaseModel, int64, error) {
	query, err := r.query(ctx, &FallbackTargetLeaseModel{})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []FallbackTargetLeaseModel
	err = query.Order("expires_at DESC, id DESC").Offset(page.Offset()).Limit(page.Limit).Find(&items).Error
	if err == nil {
		now := time.Now().UTC().Unix()
		for index := range items {
			if items[index].State == "ACTIVE" && items[index].ExpiresAt <= now {
				items[index].State = "STALE"
				var reasons []string
				_ = json.Unmarshal(items[index].ReasonCodesJSON, &reasons)
				reasons = appendUniqueReason(reasons, "fallback_target_lease_stale")
				items[index].ReasonCodesJSON, _ = json.Marshal(reasons)
			}
		}
	}
	return items, total, err
}

func appendUniqueReason(values []string, reason string) []string {
	for _, value := range values {
		if value == reason {
			return values
		}
	}
	return append(values, reason)
}
func migrateV2Compatibility(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	for after := uint(0); ; {
		var rows []ProbeEventModel
		if err := db.Where("id > ?", after).Order("id ASC").Limit(500).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			var safe domain.SafeMeta
			if err := json.Unmarshal(row.SafeMetaJSON, &safe); err != nil {
				safe = domain.SafeMeta{ClassifierPolicyVersion: domain.ClassifierPolicyVersion, Truncated: true}
			}
			family := 0
			if row.IPFamily != nil {
				family = *row.IPFamily
			}
			event := protectionevents.ProbeEvent{ResourceID: row.ResourceID, ResourceKind: domain.ResourceKind(row.ResourceKind), SourcePrefix: row.SourceIPCIDR, IPFamily: family, SignalKind: domain.SignalKind(row.SignalKind), ScoreDelta: row.ScoreDelta, Action: domain.DecisionAction(row.Action), SafeMeta: safe, ObservedAt: time.Unix(row.ObservedAt, 0).UTC(), DedupeKey: row.DedupeKey}
			signal := protectionevents.NormalizeProbeEvent(event, protectionevents.ProbeEventID(row.ID))
			if err := saveSignalTx(db, signal); err != nil {
				return fmt.Errorf("migrate probe event %d: %w", row.ID, err)
			}
			after = row.ID
		}
		if len(rows) < 500 {
			break
		}
	}
	for after := uint(0); ; {
		var rows []ScoreStateModel
		if err := db.Where("id > ?", after).Order("id ASC").Limit(500).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			state, err := scoreDomain(row)
			if err != nil {
				after = row.ID
				continue
			}
			decision := protectiondecision.NormalizeScoreState(state, row.LastDecision, time.Now().UTC())
			if err := saveDecisionTx(db, decision); err != nil {
				return fmt.Errorf("migrate score state %d: %w", row.ID, err)
			}
			after = row.ID
		}
		if len(rows) < 500 {
			break
		}
	}
	globalLimit, _ := contractRetentionLimits(db)
	if err := pruneSignalContracts(db, globalLimit, globalLimit, ""); err != nil {
		return err
	}
	if err := pruneDecisionContracts(db, globalLimit); err != nil {
		return err
	}
	return pruneLeaseContracts(db, neutralfallback.MaxReferenceLeases)
}

func contractRetentionLimits(db *gorm.DB) (int, int) {
	globalLimit := domain.DefaultRetentionGlobalLimit
	perTargetLimit := domain.DefaultRetentionPerResourceLimit
	var settings SettingsModel
	if db != nil && db.Where("id = ?", 1).First(&settings).Error == nil {
		if settings.RetentionGlobalLimit > 0 {
			globalLimit = min(settings.RetentionGlobalLimit, 100000)
		}
		if settings.RetentionPerResourceLimit > 0 {
			perTargetLimit = min(settings.RetentionPerResourceLimit, globalLimit)
		}
	}
	return max(globalLimit, 1), max(min(perTargetLimit, globalLimit), 1)
}

func pruneSignalContracts(db *gorm.DB, globalLimit, perTargetLimit int, targetResourceID string) error {
	if db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	excess := db.Model(&ProtectionSignalV2Model{}).Select("id").Order("observed_at DESC, id DESC").Offset(max(globalLimit, 1)).Limit(-1)
	if err := db.Where("id IN (?)", excess).Delete(&ProtectionSignalV2Model{}).Error; err != nil {
		return err
	}
	if strings.TrimSpace(targetResourceID) == "" {
		return nil
	}
	perTargetExcess := db.Model(&ProtectionSignalV2Model{}).Select("id").Where("target_resource_id = ?", targetResourceID).Order("observed_at DESC, id DESC").Offset(max(perTargetLimit, 1)).Limit(-1)
	return db.Where("id IN (?)", perTargetExcess).Delete(&ProtectionSignalV2Model{}).Error
}

func pruneDecisionContracts(db *gorm.DB, globalLimit int) error {
	if db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	excess := db.Model(&ProtectionDecisionV2Model{}).Select("id").Order("created_at DESC, id DESC").Offset(max(globalLimit, 1)).Limit(-1)
	return db.Where("id IN (?)", excess).Delete(&ProtectionDecisionV2Model{}).Error
}

func pruneLeaseContracts(db *gorm.DB, limit int) error {
	if db == nil || limit < 0 {
		return errors.New("server-protection repository is not initialized")
	}
	var count int64
	if err := db.Model(&FallbackTargetLeaseModel{}).Count(&count).Error; err != nil {
		return err
	}
	if count <= int64(limit) {
		return nil
	}
	excess := int(count - int64(limit))
	candidates := db.Model(&FallbackTargetLeaseModel{}).Select("id").Where("state <> ? OR expires_at <= ?", "ACTIVE", time.Now().UTC().Unix()).Order("expires_at ASC, id ASC").Limit(excess)
	if err := db.Where("id IN (?)", candidates).Delete(&FallbackTargetLeaseModel{}).Error; err != nil {
		return err
	}
	if err := db.Model(&FallbackTargetLeaseModel{}).Count(&count).Error; err != nil {
		return err
	}
	if count > int64(limit) {
		return errors.New("fallback_target_lease_capacity_exceeded")
	}
	return nil
}

func saveSignalTx(db *gorm.DB, signal domain.ProtectionSignalV2) error {
	now := time.Now().UTC()
	if signal.ObservedAt.After(now.Add(5 * time.Minute)) {
		signal.ObservedAt = now
		signal.ExpiresAt = now.Add(time.Hour)
		signal.ConfidenceBP = 0
		signal.ReasonCodes = appendUniqueReason(signal.ReasonCodes, domain.ReasonUnknown)
	}
	if err := signal.Validate(now); err != nil {
		return err
	}
	payload, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	meta, _ := json.Marshal(signal.SafeMeta)
	model := ProtectionSignalV2Model{SignalID: signal.SignalID, Schema: signal.Schema, SourceID: signal.Source.SourceID, SourceClass: signal.Source.SourceClass, Category: string(signal.Category), Kind: signal.Kind, KnownKind: signal.KnownKind, SubjectType: signal.Subject.Type, SubjectValue: signal.Subject.Value, Scope: string(signal.Scope.Scope), TargetResourceID: signal.Scope.TargetResourceID, EndpointID: signal.Scope.EndpointID, Transport: signal.Scope.Transport, ObservationWindowID: signal.Provenance.ObservationWindowID, ObservedAt: signal.ObservedAt.Unix(), ExpiresAt: signal.ExpiresAt.Unix(), ConfidenceBP: signal.ConfidenceBP, PolicyRevision: signal.Provenance.PolicyRevision, SafeMetaBytes: len(meta), ContractJSON: payload}
	return createSignalIdempotent(db, model)
}

func createSignalIdempotent(db *gorm.DB, model ProtectionSignalV2Model) error {
	var existing ProtectionSignalV2Model
	err := db.Where("signal_id = ?", model.SignalID).First(&existing).Error
	if err == nil {
		if bytes.Equal(existing.ContractJSON, model.ContractJSON) {
			return nil
		}
		return errors.New("conflicting signal replay")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&model).Error
}
func saveDecisionTx(db *gorm.DB, decision domain.ProtectionDecisionV2) error {
	now := time.Now().UTC()
	if decision.CreatedAt.After(now.Add(5 * time.Minute)) {
		decision.CreatedAt = now
		decision.ExpiresAt = now.Add(time.Hour)
		decision.ScoreSnapshot.CapturedAt = now
		decision.ConfidenceBP = 0
		decision.State = domain.DecisionDegraded
		decision.ReasonCodes = appendUniqueReason(decision.ReasonCodes, domain.ReasonUnknown)
	}
	if err := decision.Validate(now); err != nil {
		return err
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	model := ProtectionDecisionV2Model{DecisionID: decision.DecisionID, Schema: decision.Schema, PolicyRevision: decision.PolicyRevision, SubjectType: decision.Subject.Type, SubjectValue: decision.Subject.Value, Scope: string(decision.Scope.Scope), RequestedIntent: string(decision.RequestedIntent), ResolvedIntent: string(decision.CapabilityResolution.ResolvedIntent), ActionImplemented: false, State: string(decision.State), CreatedAt: decision.CreatedAt.Unix(), ExpiresAt: decision.ExpiresAt.Unix(), ContractJSON: payload}
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "decision_id"}}, DoNothing: true}).Create(&model).Error
}

var _ neutralfallback.LeaseStore = (*Repository)(nil)
