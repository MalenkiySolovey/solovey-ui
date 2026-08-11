package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) LoadScore(ctx context.Context, key scoring.ScoreKey) (scoring.ScoreState, error) {
	if r == nil || r.db == nil {
		return scoring.ScoreState{}, errors.New("server-protection repository is not initialized")
	}
	if key.ResourceID == "" || !key.Prefix.IsValid() {
		return scoring.ScoreState{}, errors.New("score key is invalid")
	}
	var model ScoreStateModel
	err := r.db.WithContext(ctx).Where("resource_id = ? AND source_prefix = ?", key.ResourceID, key.Prefix.String()).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return scoring.ScoreState{}, ErrScoreNotFound
	}
	if err != nil {
		return scoring.ScoreState{}, err
	}
	return scoreDomain(model)
}

func (r *Repository) SaveScore(ctx context.Context, state scoring.ScoreState) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	if state.ResourceID == "" || !state.SourcePrefix.IsValid() {
		return errors.New("score state key is invalid")
	}
	if err := state.LastDecision.Validate(); err != nil {
		return err
	}
	reasons, err := json.Marshal(state.Reasons)
	if err != nil {
		return err
	}
	dedupe, err := json.Marshal(scoreDedupe{
		Key: state.LastDedupeKey, At: state.LastDedupeAt.Unix(), Count: state.DedupedCount,
	})
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	model := ScoreStateModel{
		ResourceID: state.ResourceID, SourcePrefix: state.SourcePrefix.String(),
		IPFamily: ipFamily(state.SourcePrefix.Addr()), CurrentScore: state.CurrentScore,
		RawScore: state.RawScore, FirstSeenAt: state.FirstSeenAt.Unix(),
		LastSignalAt: state.LastSignalAt.Unix(), ExpiresAt: optionalUnix(state.ExpiresAt),
		ReasonsJSON: reasons, LastDecision: string(state.LastDecision), DedupeJSON: dedupe,
		ClassifierPolicyVersion: state.ClassifierPolicyVersion, CreatedAt: now, UpdatedAt: now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "resource_id"}, {Name: "source_prefix"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"ip_family", "current_score", "raw_score", "first_seen_at", "last_signal_at",
			"expires_at", "reasons_json", "last_decision", "dedupe_json",
			"classifier_policy_version", "updated_at",
		}),
	}).Create(&model).Error
}

func (r *Repository) PersistObservationBatch(ctx context.Context, states []scoring.ScoreState, values []events.ProbeEvent, clear []scoring.ScoreKey) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		batch := New(tx)
		for _, key := range clear {
			if key.ResourceID == "" || !key.Prefix.IsValid() {
				continue
			}
			if err := tx.Where("resource_id = ? AND source_prefix = ?", key.ResourceID, key.Prefix.String()).Delete(&ScoreStateModel{}).Error; err != nil {
				return err
			}
			if err := tx.Where("resource_id = ? AND ip_cidr = ?", key.ResourceID, key.Prefix.String()).Delete(&GraylistModel{}).Error; err != nil {
				return err
			}
		}
		for _, state := range states {
			if err := batch.SaveScore(ctx, state); err != nil {
				return err
			}
			if err := upsertGraylist(tx, state); err != nil {
				return err
			}
		}
		return batch.AppendBatch(ctx, values)
	})
}

func (r *Repository) ActiveIPAllowlist(ctx context.Context, now time.Time) ([]netip.Prefix, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	var items []IPAllowlistModel
	if err := r.db.WithContext(ctx).Where("expires_at IS NULL OR expires_at > ?", now.Unix()).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	result := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		prefix, err := netip.ParsePrefix(item.IPCIDR)
		if err != nil {
			continue
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func upsertGraylist(tx *gorm.DB, state scoring.ScoreState) error {
	if state.ResourceID == "" || !state.SourcePrefix.IsValid() {
		return nil
	}
	if state.ExpiresAt == nil {
		return tx.Where("resource_id = ? AND ip_cidr = ?", state.ResourceID, state.SourcePrefix.String()).Delete(&GraylistModel{}).Error
	}
	now := time.Now().Unix()
	reason, lastSignal := "threshold", "classified"
	if len(state.Reasons) > 0 {
		lastSignal = string(state.Reasons[0].Kind)
		if state.Reasons[0].SafeLabel != "" {
			reason = state.Reasons[0].SafeLabel
		} else {
			reason = lastSignal
		}
	}
	model := GraylistModel{
		ResourceID: state.ResourceID, IPCIDR: state.SourcePrefix.String(), IPFamily: ipFamily(state.SourcePrefix.Addr()),
		Score: state.CurrentScore, Reason: reason, LastSignal: lastSignal, ExpiresAt: state.ExpiresAt.Unix(), CreatedAt: now, UpdatedAt: now,
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "resource_id"}, {Name: "ip_cidr"}},
		DoUpdates: clause.AssignmentColumns([]string{"ip_family", "score", "reason", "last_signal", "expires_at", "updated_at"}),
	}).Create(&model).Error
}

type scoreDedupe struct {
	Key   string `json:"key,omitempty"`
	At    int64  `json:"at,omitempty"`
	Count int    `json:"count,omitempty"`
}

func scoreDomain(model ScoreStateModel) (scoring.ScoreState, error) {
	prefix, err := netip.ParsePrefix(model.SourcePrefix)
	if err != nil {
		return scoring.ScoreState{}, err
	}
	var reasons []scoring.ScoreReason
	if err := json.Unmarshal(model.ReasonsJSON, &reasons); err != nil {
		return scoring.ScoreState{}, err
	}
	var dedupe scoreDedupe
	if len(model.DedupeJSON) > 0 {
		if err := json.Unmarshal(model.DedupeJSON, &dedupe); err != nil {
			return scoring.ScoreState{}, err
		}
	}
	state := scoring.ScoreState{
		ResourceID: model.ResourceID, SourcePrefix: prefix,
		CurrentScore: model.CurrentScore, RawScore: model.RawScore,
		FirstSeenAt: time.Unix(model.FirstSeenAt, 0), LastSignalAt: time.Unix(model.LastSignalAt, 0),
		Reasons: reasons, LastDecision: domain.DecisionAction(model.LastDecision),
		LastDedupeKey: dedupe.Key, DedupedCount: dedupe.Count,
		ClassifierPolicyVersion: model.ClassifierPolicyVersion,
	}
	if dedupe.At > 0 {
		state.LastDedupeAt = time.Unix(dedupe.At, 0)
	}
	if model.ExpiresAt != nil {
		expires := time.Unix(*model.ExpiresAt, 0)
		state.ExpiresAt = &expires
	}
	return state, nil
}

func optionalIPFamily(value int) *int {
	if value != 4 && value != 6 {
		return nil
	}
	return &value
}

func ipFamily(addr netip.Addr) int {
	if addr.Unmap().Is4() {
		return 4
	}
	return 6
}

func optionalUnix(value *time.Time) *int64 {
	if value == nil {
		return nil
	}
	unix := value.Unix()
	return &unix
}
