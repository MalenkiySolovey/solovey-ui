//go:build !minimal

package authority

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"gorm.io/gorm"
)

const (
	ProviderID             = "fallback-html"
	ReservationTable       = "fallback_html_target_reservations"
	ReservationReplayTable = "fallback_html_target_reservation_requests"
	SlotsPerTarget         = uint32(4)
	MaxBlockedIDs          = 16
)

var mutationToken = func() chan struct{} {
	token := make(chan struct{}, 1)
	token <- struct{}{}
	return token
}()

type ReservationModel struct {
	ReservationID       string `gorm:"column:reservation_id;primaryKey;size:128"`
	Schema              string `gorm:"column:schema;size:128;not null"`
	ReservationRevision string `gorm:"column:reservation_revision;size:128;not null;index"`
	ProviderID          string `gorm:"column:provider_id;size:128;not null;index:idx_fallback_html_reservation_target,priority:1"`
	TargetID            string `gorm:"column:target_id;size:128;not null;index:idx_fallback_html_reservation_target,priority:2"`
	HolderID            string `gorm:"column:holder_id;size:128;not null;index"`
	Purpose             string `gorm:"column:purpose;size:64;not null"`
	TargetReferenceJSON string `gorm:"column:target_reference_json;type:text;not null"`
	State               string `gorm:"column:state;size:32;not null;index"`
	IssuedAt            int64  `gorm:"column:issued_at;not null"`
	RenewedAt           int64  `gorm:"column:renewed_at;not null"`
	FreshnessExpiresAt  int64  `gorm:"column:freshness_expires_at;not null"`
	ReleasedAt          int64  `gorm:"column:released_at;not null;default:0"`
	ReasonCodesJSON     string `gorm:"column:reason_codes_json;type:text;not null"`
	CreatedAt           int64  `gorm:"column:created_at;not null"`
	UpdatedAt           int64  `gorm:"column:updated_at;not null"`
}

func (ReservationModel) TableName() string { return ReservationTable }

type ReservationReplayModel struct {
	RequestID       string `gorm:"column:request_id;primaryKey;size:128"`
	RequestRevision string `gorm:"column:request_revision;size:64;not null"`
	ReservationID   string `gorm:"column:reservation_id;size:128;not null;index"`
	ResultJSON      string `gorm:"column:result_json;type:text;not null"`
	CreatedAt       int64  `gorm:"column:created_at;not null"`
}

func (ReservationReplayModel) TableName() string { return ReservationReplayTable }

type MutationBlockedError struct {
	ReservationIDs []string
	ReasonCode     string
}

func (e *MutationBlockedError) Error() string {
	if e == nil {
		return ""
	}
	return "fallback-html target mutation blocked by provider reservation"
}

func EnsureSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := db.AutoMigrate(&ReservationModel{}, &ReservationReplayModel{}); err != nil {
		return err
	}
	for _, query := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_fallback_html_reservation_revision ON fallback_html_target_reservations(reservation_id, reservation_revision)",
		"CREATE INDEX IF NOT EXISTS idx_fallback_html_reservation_guard ON fallback_html_target_reservations(provider_id, target_id, state, freshness_expires_at)",
	} {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func DropSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	for _, model := range []any{&ReservationReplayModel{}, &ReservationModel{}} {
		if db.Migrator().HasTable(model) {
			if err := db.Migrator().DropTable(model); err != nil {
				return err
			}
		}
	}
	return nil
}

func WithMutationLock(fn func() error) error {
	return WithMutationLockContext(context.Background(), fn)
}

func WithMutationLockContext(ctx context.Context, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-mutationToken:
	}
	defer func() { mutationToken <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func NewOpaqueID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func RequestRevision(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func EncodeReservation(reservation neutral.ProviderTargetReservationV1, createdAt, updatedAt int64) (ReservationModel, error) {
	reservation.ReasonCodes = canonicalReasons(reservation.ReasonCodes)
	if err := reservation.Validate(); err != nil {
		return ReservationModel{}, err
	}
	if reservation.ExactTargetReference.ProviderID != ProviderID {
		return ReservationModel{}, errors.New("fallback_html_reservation_provider_invalid")
	}
	referenceJSON, err := json.Marshal(reservation.ExactTargetReference)
	if err != nil {
		return ReservationModel{}, err
	}
	reasonsJSON, err := json.Marshal(reservation.ReasonCodes)
	if err != nil {
		return ReservationModel{}, err
	}
	return ReservationModel{
		ReservationID: reservation.ReservationID, Schema: reservation.Schema,
		ReservationRevision: reservation.ReservationRevision,
		ProviderID:          reservation.ExactTargetReference.ProviderID,
		TargetID:            reservation.ExactTargetReference.TargetID,
		HolderID:            reservation.HolderID, Purpose: string(reservation.Purpose),
		TargetReferenceJSON: string(referenceJSON), State: string(reservation.State),
		IssuedAt: reservation.IssuedAt, RenewedAt: reservation.RenewedAt,
		FreshnessExpiresAt: reservation.FreshnessExpiresAt, ReleasedAt: reservation.ReleasedAt,
		ReasonCodesJSON: string(reasonsJSON), CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func DecodeReservation(row ReservationModel) (neutral.ProviderTargetReservationV1, error) {
	var reference neutral.FallbackTargetReferenceV2
	var reasons []string
	if err := json.Unmarshal([]byte(row.TargetReferenceJSON), &reference); err != nil {
		return neutral.ProviderTargetReservationV1{}, errors.New("fallback_html_reservation_reference_invalid")
	}
	if err := json.Unmarshal([]byte(row.ReasonCodesJSON), &reasons); err != nil {
		return neutral.ProviderTargetReservationV1{}, errors.New("fallback_html_reservation_reasons_invalid")
	}
	reservation := neutral.ProviderTargetReservationV1{
		Schema: row.Schema, ReservationID: row.ReservationID,
		ReservationRevision: row.ReservationRevision, HolderID: row.HolderID,
		Purpose: neutral.ReservationPurpose(row.Purpose), ExactTargetReference: reference,
		State: neutral.ReservationState(row.State), IssuedAt: row.IssuedAt,
		RenewedAt: row.RenewedAt, FreshnessExpiresAt: row.FreshnessExpiresAt,
		ReleasedAt: row.ReleasedAt, ReasonCodes: canonicalReasons(reasons),
	}
	if reference.ProviderID != row.ProviderID || reference.TargetID != row.TargetID {
		return neutral.ProviderTargetReservationV1{}, errors.New("fallback_html_reservation_reference_mismatch")
	}
	if err := reservation.Validate(); err != nil || reservation.ExactTargetReference.ProviderID != ProviderID {
		return neutral.ProviderTargetReservationV1{}, errors.New("fallback_html_reservation_record_invalid")
	}
	return reservation, nil
}

func LoadReplay(tx *gorm.DB, requestID, requestRevision string) (neutral.ProviderTargetReservationV1, bool, error) {
	var replay ReservationReplayModel
	err := tx.Where("request_id = ?", requestID).First(&replay).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return neutral.ProviderTargetReservationV1{}, false, nil
	}
	if err != nil {
		return neutral.ProviderTargetReservationV1{}, false, err
	}
	if replay.RequestRevision != requestRevision {
		return neutral.ProviderTargetReservationV1{}, true, errors.New("fallback_html_reservation_replay_conflict")
	}
	result, err := decodeReplayResult(replay)
	if err != nil {
		return neutral.ProviderTargetReservationV1{}, true, errors.New("fallback_html_reservation_replay_invalid")
	}
	var currentRow ReservationModel
	if err := tx.Where("reservation_id = ?", result.ReservationID).First(&currentRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return neutral.ProviderTargetReservationV1{}, true, errors.New("fallback_html_reservation_replay_invalid")
		}
		return neutral.ProviderTargetReservationV1{}, true, err
	}
	current, err := DecodeReservation(currentRow)
	if err != nil || !sameReservationAuthority(result, current) {
		return neutral.ProviderTargetReservationV1{}, true, errors.New("fallback_html_reservation_replay_invalid")
	}
	if err := neutral.ValidateReplayRequest(requestID, replay.RequestRevision, requestRevision, result, result); err != nil {
		return neutral.ProviderTargetReservationV1{}, true, err
	}
	return result, true, nil
}

func decodeReplayResult(replay ReservationReplayModel) (neutral.ProviderTargetReservationV1, error) {
	var result neutral.ProviderTargetReservationV1
	if err := json.Unmarshal([]byte(replay.ResultJSON), &result); err != nil || result.ReservationID != replay.ReservationID || result.Validate() != nil || result.ExactTargetReference.ProviderID != ProviderID {
		return neutral.ProviderTargetReservationV1{}, errors.New("fallback_html_reservation_replay_invalid")
	}
	return result, nil
}

func sameReservationAuthority(left, right neutral.ProviderTargetReservationV1) bool {
	return left.Schema == right.Schema &&
		left.ReservationID == right.ReservationID &&
		left.HolderID == right.HolderID &&
		left.Purpose == right.Purpose &&
		left.IssuedAt == right.IssuedAt &&
		left.ExactTargetReference == right.ExactTargetReference
}

func SaveReplay(tx *gorm.DB, requestID, requestRevision string, result neutral.ProviderTargetReservationV1, now int64) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return tx.Create(&ReservationReplayModel{
		RequestID: requestID, RequestRevision: requestRevision,
		ReservationID: result.ReservationID, ResultJSON: string(payload), CreatedAt: now,
	}).Error
}

func CountGuardingTarget(tx *gorm.DB, providerID, targetID string, now time.Time) (uint32, error) {
	counts, _, err := GuardingCounts(tx, providerID, now)
	if err != nil {
		return 0, err
	}
	return counts[targetID], nil
}

func GuardingCounts(tx *gorm.DB, providerID string, now time.Time) (map[string]uint32, uint32, error) {
	var rows []ReservationModel
	if err := tx.Order("reservation_id ASC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	counts := make(map[string]uint32)
	var total uint32
	for _, row := range rows {
		reservation, err := DecodeReservation(row)
		if err != nil {
			return nil, 0, err
		}
		if reservation.ExactTargetReference.ProviderID != providerID {
			return nil, 0, errors.New("fallback_html_reservation_provider_invalid")
		}
		if reservation.Status(now).BlocksMutation {
			counts[reservation.ExactTargetReference.TargetID]++
			total++
		}
	}
	return counts, total, nil
}

func CountAllGuarding(tx *gorm.DB, now time.Time) (uint32, error) {
	_, total, err := GuardingCounts(tx, ProviderID, now)
	return total, err
}

func GuardSiteMutation(tx *gorm.DB, providerID, targetID string, now time.Time) error {
	if !tx.Migrator().HasTable(&ReservationModel{}) {
		return nil
	}
	var rows []ReservationModel
	if err := tx.Order("reservation_id ASC").Find(&rows).Error; err != nil {
		return err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		reservation, err := DecodeReservation(row)
		if err != nil || reservation.ExactTargetReference.ProviderID != providerID {
			return &MutationBlockedError{ReasonCode: "reservation_record_invalid"}
		}
		if reservation.ExactTargetReference.TargetID == targetID && reservation.Status(now).BlocksMutation && len(ids) < MaxBlockedIDs {
			ids = append(ids, reservation.ReservationID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return &MutationBlockedError{ReservationIDs: ids, ReasonCode: "target_reserved"}
}

func GuardAllMutation(tx *gorm.DB, now time.Time) error {
	if !tx.Migrator().HasTable(&ReservationModel{}) {
		return nil
	}
	var rows []ReservationModel
	if err := tx.Order("reservation_id ASC").Find(&rows).Error; err != nil {
		return err
	}
	return guardRows(rows, now)
}

func guardRows(rows []ReservationModel, now time.Time) error {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		reservation, err := DecodeReservation(row)
		if err != nil {
			return &MutationBlockedError{ReasonCode: "reservation_record_invalid"}
		}
		if reservation.Status(now).BlocksMutation && len(ids) < MaxBlockedIDs {
			ids = append(ids, reservation.ReservationID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return &MutationBlockedError{ReservationIDs: ids, ReasonCode: "target_reserved"}
}

func ReconcileRestoredInTx(ctx context.Context, tx *gorm.DB, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var rows []ReservationModel
	if err := tx.WithContext(ctx).Order("reservation_id ASC").Find(&rows).Error; err != nil {
		return err
	}
	counts := map[string]uint32{}
	decoded := make(map[string]neutral.ProviderTargetReservationV1, len(rows))
	var total uint32
	for _, row := range rows {
		reservation, err := DecodeReservation(row)
		if err != nil {
			return err
		}
		if err := reservation.ValidateAt(now); err != nil {
			return errors.New("fallback_html_restored_reservation_invalid")
		}
		decoded[reservation.ReservationID] = reservation
		if reservation.State != neutral.ReservationReleased {
			key := reservation.ExactTargetReference.ProviderID + "\x00" + reservation.ExactTargetReference.TargetID
			counts[key]++
			total++
			if counts[key] > SlotsPerTarget || total > neutral.MaxReservationsV2 {
				return errors.New("fallback_html_restored_reservation_capacity_invalid")
			}
		}
	}
	if tx.Migrator().HasTable(&ReservationReplayModel{}) {
		var replays []ReservationReplayModel
		if err := tx.WithContext(ctx).Order("request_id ASC").Find(&replays).Error; err != nil {
			return err
		}
		for _, replay := range replays {
			result, err := decodeReplayResult(replay)
			current, found := decoded[result.ReservationID]
			if err != nil || !found || !sameReservationAuthority(result, current) {
				return errors.New("fallback_html_restored_reservation_replay_invalid")
			}
			if err := neutral.ValidateReplayRequest(replay.RequestID, replay.RequestRevision, replay.RequestRevision, result, result); err != nil {
				return errors.New("fallback_html_restored_reservation_replay_invalid")
			}
		}
	}
	for _, row := range rows {
		reservation := decoded[row.ReservationID]
		if reservation.State == neutral.ReservationReleased || reservation.State == neutral.ReservationReconcileRequired {
			continue
		}
		next := reservation
		next.State = neutral.ReservationReconcileRequired
		next.ReasonCodes = canonicalReasons(append(next.ReasonCodes, "restored_reconciliation_required"))
		revision, err := NewOpaqueID("r-")
		if err != nil {
			return err
		}
		next.ReservationRevision = revision
		if err := neutral.ValidateReconcileTransition(reservation, next, neutral.ReservationCASV1{
			RequestID: "restore-reconcile", ReservationID: reservation.ReservationID,
			ExpectedRevision: reservation.ReservationRevision,
		}, now); err != nil {
			return err
		}
		updated, err := EncodeReservation(next, row.CreatedAt, now.Unix())
		if err != nil {
			return err
		}
		result := tx.WithContext(ctx).Model(&ReservationModel{}).
			Where("reservation_id = ? AND reservation_revision = ?", reservation.ReservationID, reservation.ReservationRevision).
			Select("reservation_revision", "state", "reason_codes_json", "updated_at").Updates(updated)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("fallback_html_restored_reservation_conflict")
		}
	}
	return nil
}

func canonicalReasons(values []string) []string {
	out := append([]string(nil), values...)
	for index := range out {
		out[index] = strings.TrimSpace(out[index])
	}
	sort.Strings(out)
	compact := out[:0]
	for _, value := range out {
		if value == "" || (len(compact) > 0 && compact[len(compact)-1] == value) {
			continue
		}
		compact = append(compact, value)
	}
	return compact
}
