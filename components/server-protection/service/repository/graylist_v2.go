package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"gorm.io/gorm"
)

var ErrGraylistStateNotFound = errors.New("graylist state not found")

func (r *Repository) LoadGraylistStateV2(ctx context.Context, stateID string) (domain.GraylistStateV2, error) {
	if r == nil || r.db == nil || !domain.ValidSHA256(stateID) {
		return domain.GraylistStateV2{}, errors.New("graylist state lookup is invalid")
	}
	var row GraylistStateV2Model
	err := r.db.WithContext(ctx).Where("state_id = ?", stateID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.GraylistStateV2{}, ErrGraylistStateNotFound
	}
	if err != nil {
		return domain.GraylistStateV2{}, err
	}
	return graylistStateDomain(row)
}

// StoreGraylistEvaluation accepts policy-evaluation output only. It cannot
// manufacture or assign APPLIED; executor projections remain separately
// verified AppliedActionV1 records.
func (r *Repository) StoreGraylistEvaluation(ctx context.Context, state domain.GraylistStateV2, expectedRevision uint64) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("server-protection repository is not initialized")
	}
	if err := state.Validate(); err != nil {
		return false, err
	}
	if state.ActualActionState != "NOT_APPLIED" || state.AppliedActionRefID != "" {
		return false, errors.New("graylist evaluation cannot assign actual action state")
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	model := graylistStateModel(state, payload)
	changed := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var storeErr error
		changed, storeErr = storeGraylistEvaluationTx(tx, state, expectedRevision, model, payload)
		return storeErr
	})
	return changed, err
}

func storeGraylistEvaluationTx(tx *gorm.DB, state domain.GraylistStateV2, expectedRevision uint64, model GraylistStateV2Model, payload []byte) (bool, error) {
	var current GraylistStateV2Model
	loadErr := tx.Where("state_id = ?", state.StateID).First(&current).Error
	if errors.Is(loadErr, gorm.ErrRecordNotFound) {
		if expectedRevision != 0 || state.Revision == 0 {
			return false, errors.New("graylist revision conflict")
		}
		if err := tx.Create(&model).Error; err != nil {
			return false, err
		}
		limit, _ := contractRetentionLimits(tx)
		return true, pruneGraylistStatesV2(tx, limit)
	}
	if loadErr != nil {
		return false, loadErr
	}
	if current.Revision != expectedRevision {
		return false, errors.New("graylist revision conflict")
	}
	if current.Revision == state.Revision && bytes.Equal(current.ContractJSON, payload) {
		return false, nil
	}
	if state.Revision <= current.Revision {
		return false, errors.New("graylist revision did not advance")
	}
	model.ID = current.ID
	result := tx.Model(&GraylistStateV2Model{}).Where("id = ? AND revision = ?", current.ID, expectedRevision).Updates(map[string]any{
		"revision": state.Revision, "score": state.Score, "confidence_bp": state.ConfidenceBP,
		"band": string(state.Band), "lifecycle": string(state.Lifecycle),
		"selected_response": string(state.SelectedResponse), "desired_action": string(state.DesiredAction),
		"actual_action_state": state.ActualActionState, "applied_action_ref_id": "",
		"entered_at": state.EnteredAt.Unix(), "last_signal_at": state.LastSignalAt.Unix(),
		"expires_at": state.ExpiresAt.Unix(), "updated_at": state.UpdatedAt.Unix(),
		"signal_ref_count": len(state.SignalRefs), "reason_count": len(state.ReasonCodes),
		"contract_json": payload,
	})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, errors.New("graylist revision conflict")
	}
	return true, nil
}

func pruneGraylistStatesV2(tx *gorm.DB, limit int) error {
	if tx == nil {
		return errors.New("server-protection repository is not initialized")
	}
	limit = max(1, min(limit, 100000))
	var count int64
	if err := tx.Model(&GraylistStateV2Model{}).Count(&count).Error; err != nil || count <= int64(limit) {
		return err
	}
	excess := int(count - int64(limit))
	candidates := tx.Model(&GraylistStateV2Model{}).Select("id").
		Where("lifecycle IN ?", []string{string(domain.GraylistLifecycleExpired), string(domain.GraylistLifecycleSuperseded), string(domain.GraylistLifecycleLegacyStale)}).
		Order("updated_at ASC, id ASC").Limit(excess)
	if err := tx.Where("id IN (?)", candidates).Delete(&GraylistStateV2Model{}).Error; err != nil {
		return err
	}
	if err := tx.Model(&GraylistStateV2Model{}).Count(&count).Error; err != nil {
		return err
	}
	if count > int64(limit) {
		return errors.New("graylist subject capacity exceeded")
	}
	return nil
}

func graylistStateModel(state domain.GraylistStateV2, payload []byte) GraylistStateV2Model {
	return GraylistStateV2Model{
		StateID: state.StateID, Schema: state.Schema, Revision: state.Revision,
		SubjectType: state.Subject.Type, SubjectValue: state.Subject.Value,
		ResourceID: state.ResourceID, EndpointID: state.EndpointID, Transport: state.Transport,
		PolicyRevision: state.PolicyRevision, StrategyRevision: state.StrategyRevision, CapabilityRevision: state.CapabilityRevision,
		Score: state.Score, ConfidenceBP: state.ConfidenceBP, Band: string(state.Band), Lifecycle: string(state.Lifecycle),
		SelectedResponse: string(state.SelectedResponse), DesiredAction: string(state.DesiredAction),
		ActualActionState: state.ActualActionState, AppliedActionRefID: state.AppliedActionRefID,
		EnteredAt: state.EnteredAt.Unix(), LastSignalAt: state.LastSignalAt.Unix(), ExpiresAt: state.ExpiresAt.Unix(),
		CreatedAt: state.CreatedAt.Unix(), UpdatedAt: state.UpdatedAt.Unix(),
		SignalRefCount: len(state.SignalRefs), ReasonCount: len(state.ReasonCodes), ContractJSON: payload,
	}
}

func graylistStateDomain(row GraylistStateV2Model) (domain.GraylistStateV2, error) {
	var state domain.GraylistStateV2
	if len(row.ContractJSON) == 0 || json.Unmarshal(row.ContractJSON, &state) != nil {
		return domain.GraylistStateV2{}, errors.New("graylist state contract is invalid")
	}
	if state.StateID != row.StateID || state.Revision != row.Revision || string(state.Lifecycle) != row.Lifecycle ||
		state.ActualActionState != row.ActualActionState || state.UpdatedAt.Unix() != row.UpdatedAt {
		return domain.GraylistStateV2{}, errors.New("graylist state columns disagree with contract")
	}
	if err := state.Validate(); err != nil {
		return domain.GraylistStateV2{}, err
	}
	return state, nil
}

func migrateLegacyGraylistV2(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&GraylistModel{}) || !db.Migrator().HasTable(&GraylistStateV2Model{}) {
		return nil
	}
	var rows []GraylistModel
	if err := db.Order("id ASC").Limit(100000).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		subject := domain.SignalSubjectV2{Type: "prefix", Value: row.IPCIDR}
		prefix, err := netip.ParsePrefix(row.IPCIDR)
		if err != nil {
			sum := sha256.Sum256([]byte(row.IPCIDR))
			subject = domain.SignalSubjectV2{Type: "endpoint", Value: "legacy-subject:" + hex.EncodeToString(sum[:])}
		} else {
			subject.Value = prefix.Masked().String()
		}
		created := time.Unix(max(row.CreatedAt, 1), 0).UTC()
		updated := time.Unix(max(max(row.UpdatedAt, row.CreatedAt), 1), 0).UTC()
		reasons := []string{"legacy_scope_revision_missing", "legacy_action_not_applied"}
		if updated.After(created.Add(24 * time.Hour)) {
			created = updated
			reasons = append(reasons, "legacy_timestamp_window_normalized")
		}
		if err != nil {
			reasons = append(reasons, "legacy_subject_invalid")
		}
		expires := time.Unix(max(row.ExpiresAt, updated.Add(time.Second).Unix()), 0).UTC()
		if expires.After(created.Add(24 * time.Hour)) {
			expires = created.Add(24 * time.Hour)
		}
		if !expires.After(updated) {
			expires = updated.Add(time.Second)
		}
		state := domain.GraylistStateV2{
			Schema: domain.GraylistStateSchemaV2, Revision: 1, Subject: subject,
			ResourceID: boundedLegacyResourceID(row.ResourceID), EndpointID: boundedLegacyResourceID(row.ResourceID), Transport: "tcp",
			Score: min(max(row.Score, 0), 100), ConfidenceBP: 0,
			PolicyRevision: "legacy-policy-v1", StrategyRevision: "legacy-strategy-unknown", CapabilityRevision: "legacy-capability-unknown",
			SignalRefs: []string{}, SourceClasses: []string{"native"}, Band: domain.GraylistBandObserve, Lifecycle: domain.GraylistLifecycleLegacyStale,
			EnteredAt: created, LastSignalAt: updated, ExpiresAt: expires,
			SelectedResponse: domain.IntentObserve, DesiredAction: domain.IntentObserve, ActualActionState: "NOT_APPLIED",
			ReasonCodes: domain.CanonicalBoundedReasons(reasons...),
			CreatedAt:   created, UpdatedAt: updated,
		}
		state.FinalizeID()
		if err := state.Validate(); err != nil {
			continue
		}
		payload, _ := json.Marshal(state)
		model := graylistStateModel(state, payload)
		if err := db.Where("state_id = ?", state.StateID).FirstOrCreate(&model).Error; err != nil {
			return fmt.Errorf("migrate legacy graylist row %d: %w", row.ID, err)
		}
	}
	return nil
}

// ReconcileRestoredGraylistStates distrusts active-like historical policy
// state. It never creates an action and is idempotent.
func ReconcileRestoredGraylistStates(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&GraylistStateV2Model{}) {
		return nil
	}
	var rows []GraylistStateV2Model
	if err := db.WithContext(ctx).Where("lifecycle IN ?", []string{string(domain.GraylistLifecycleActive), string(domain.GraylistLifecycleCooldown)}).Order("id ASC").Limit(100000).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		state, err := graylistStateDomain(row)
		if err != nil {
			if updateErr := db.WithContext(ctx).Model(&GraylistStateV2Model{}).Where("id = ?", row.ID).Updates(map[string]any{
				"lifecycle": string(domain.GraylistLifecycleSuperseded), "selected_response": string(domain.IntentObserve),
				"desired_action": string(domain.IntentObserve), "actual_action_state": "NOT_APPLIED", "applied_action_ref_id": "",
			}).Error; updateErr != nil {
				return updateErr
			}
			continue
		}
		state.Lifecycle = domain.GraylistLifecycleSuperseded
		state.SelectedResponse = domain.IntentObserve
		state.DesiredAction = domain.IntentObserve
		state.ActualActionState = "NOT_APPLIED"
		state.AppliedActionRefID = ""
		state.ReasonCodes = domain.CanonicalBoundedReasons(append(state.ReasonCodes, "restored_state_untrusted")...)
		state.UpdatedAt = maxTimeRepository(now.UTC(), state.LastSignalAt)
		state.Revision++
		payload, _ := json.Marshal(state)
		if err := db.WithContext(ctx).Model(&GraylistStateV2Model{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Updates(map[string]any{
			"revision": state.Revision, "lifecycle": string(state.Lifecycle), "selected_response": string(state.SelectedResponse),
			"desired_action": string(state.DesiredAction), "actual_action_state": "NOT_APPLIED", "applied_action_ref_id": "",
			"reason_count": len(state.ReasonCodes), "updated_at": state.UpdatedAt.Unix(), "contract_json": payload,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func expireGraylistStatesV2(ctx context.Context, tx *gorm.DB, now time.Time, limit int) (int, error) {
	if tx == nil || !tx.Migrator().HasTable(&GraylistStateV2Model{}) {
		return 0, nil
	}
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	var rows []GraylistStateV2Model
	if err := tx.WithContext(ctx).Where("lifecycle IN ? AND expires_at <= ?", []string{string(domain.GraylistLifecycleActive), string(domain.GraylistLifecycleCooldown)}, now.Unix()).Order("expires_at ASC, id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, row := range rows {
		state, err := graylistStateDomain(row)
		if err != nil {
			continue
		}
		state.Band = domain.GraylistBandCooldown
		state.Lifecycle = domain.GraylistLifecycleExpired
		state.SelectedResponse = domain.IntentObserve
		state.DesiredAction = domain.IntentObserve
		state.ActualActionState = "NOT_APPLIED"
		state.AppliedActionRefID = ""
		state.ReasonCodes = domain.CanonicalBoundedReasons(append(state.ReasonCodes, "exact_expiry")...)
		state.UpdatedAt = maxTimeRepository(now.UTC(), state.LastSignalAt)
		state.Revision++
		payload, _ := json.Marshal(state)
		result := tx.WithContext(ctx).Model(&GraylistStateV2Model{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Updates(map[string]any{
			"revision": state.Revision, "band": string(state.Band), "lifecycle": string(state.Lifecycle),
			"selected_response": string(state.SelectedResponse), "desired_action": string(state.DesiredAction),
			"actual_action_state": "NOT_APPLIED", "applied_action_ref_id": "", "reason_count": len(state.ReasonCodes),
			"updated_at": state.UpdatedAt.Unix(), "contract_json": payload,
		})
		if result.Error != nil {
			return changed, result.Error
		}
		changed += int(result.RowsAffected)
	}
	return changed, nil
}

func maxTimeRepository(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func boundedLegacyResourceID(value string) string {
	if domain.ValidContractID(value, 256) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "legacy-resource:" + hex.EncodeToString(sum[:])
}
