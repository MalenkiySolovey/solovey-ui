//go:build !minimal

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	inbounddrafts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/drafts"
	"gorm.io/gorm"
)

const (
	LegacySelfStealSource          = "fallback-html:self-steal"
	LegacySelfStealRetiredReason   = "legacy_self_steal_retired"
	LegacySelfStealInvalidReason   = "legacy_self_steal_invalid"
	LegacySelfStealRetiredStatus   = "retired"
	LegacySelfStealInvalidStatus   = "legacy_invalid"
	maxLegacySelfStealPayloadBytes = 256 * 1024
)

// LegacySelfStealInspection is intentionally non-actionable. It contains no
// target, inbound, TLS, port, path, key, certificate, or raw payload fields.
type LegacySelfStealInspection struct {
	Classification string   `json:"classification"`
	ReasonCodes    []string `json:"reasonCodes"`
}

// legacySelfStealPayloadV1 lists the old top-level keys only so historical
// rows remain readable without retaining the former write model. Potentially
// sensitive or actionable nested values are never decoded into semantics.
type legacySelfStealPayloadV1 struct {
	Schema               string          `json:"schema"`
	Source               string          `json:"source"`
	Profile              json.RawMessage `json:"profile"`
	NoApply              json.RawMessage `json:"noApply"`
	RequiresCapability   json.RawMessage `json:"requiresCapability"`
	CoreDraftID          json.RawMessage `json:"coreDraftId"`
	SiteName             json.RawMessage `json:"siteName"`
	Target               json.RawMessage `json:"target"`
	ActivePublish        json.RawMessage `json:"activePublish"`
	HandshakeHost        json.RawMessage `json:"handshakeHost"`
	PublicListen         json.RawMessage `json:"publicListen"`
	PublicPort           json.RawMessage `json:"publicPort"`
	Transport            json.RawMessage `json:"transport"`
	HandshakeTarget      json.RawMessage `json:"handshakeTarget"`
	PortTransfer         json.RawMessage `json:"portTransfer"`
	TLSRecordID          json.RawMessage `json:"tlsRecordId"`
	RealityPublicKey     json.RawMessage `json:"realityPublicKey"`
	RealityShortID       json.RawMessage `json:"realityShortId"`
	InboundType          json.RawMessage `json:"inboundType"`
	InboundTag           json.RawMessage `json:"inboundTag"`
	InboundCandidate     json.RawMessage `json:"inboundCandidate"`
	Warnings             json.RawMessage `json:"warnings"`
	Blocks               json.RawMessage `json:"blocks"`
	ConservativeDefaults json.RawMessage `json:"conservativeDefaults"`
	NextSteps            json.RawMessage `json:"nextSteps"`
}

func DecodeLegacySelfStealPayload(raw json.RawMessage) (LegacySelfStealInspection, error) {
	if len(raw) == 0 || len(raw) > maxLegacySelfStealPayloadBytes {
		return invalidLegacySelfStealInspection(), errors.New(LegacySelfStealInvalidReason)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload legacySelfStealPayloadV1
	if err := decoder.Decode(&payload); err != nil {
		return invalidLegacySelfStealInspection(), errors.New(LegacySelfStealInvalidReason)
	}
	if err := expectLegacyJSONEOF(decoder); err != nil {
		return invalidLegacySelfStealInspection(), errors.New(LegacySelfStealInvalidReason)
	}
	if payload.Schema != "solovey-ui/fallback-html-self-steal-draft/v1" ||
		payload.Source != LegacySelfStealSource {
		return invalidLegacySelfStealInspection(), errors.New(LegacySelfStealInvalidReason)
	}
	return LegacySelfStealInspection{
		Classification: "RETIRED_NON_ACTIONABLE",
		ReasonCodes:    []string{LegacySelfStealRetiredReason},
	}, nil
}

func invalidLegacySelfStealInspection() LegacySelfStealInspection {
	return LegacySelfStealInspection{
		Classification: "LEGACY_INVALID_NON_ACTIONABLE",
		ReasonCodes:    []string{LegacySelfStealRetiredReason, LegacySelfStealInvalidReason},
	}
}

func expectLegacyJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New(LegacySelfStealInvalidReason)
	}
	return err
}

// ReconcileLegacySelfSteal retires only exact historical self-steal records.
// Payloads remain byte-for-byte in their owning tables; targets, publishes,
// TLS rows, inbounds, reservations, and runtime state are never touched.
func ReconcileLegacySelfSteal(ctx context.Context, db *gorm.DB, now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	hasHistorical := db.Migrator().HasTable(&fallbackdomain.SelfStealDraft{})
	hasCore := db.Migrator().HasTable(&model.InboundDraft{})
	if !hasHistorical && !hasCore {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hasHistorical {
			var rows []fallbackdomain.SelfStealDraft
			if err := tx.WithContext(ctx).
				Where("status IN ?", []string{"blocked", "ready"}).
				Order("id ASC").Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if err := ctx.Err(); err != nil {
					return err
				}
				status := LegacySelfStealRetiredStatus
				if _, err := DecodeLegacySelfStealPayload(row.Payload); err != nil {
					status = LegacySelfStealInvalidStatus
				}
				if err := tx.WithContext(ctx).Model(&fallbackdomain.SelfStealDraft{}).
					Where("id = ? AND status IN ?", row.ID, []string{"blocked", "ready"}).
					Update("status", status).Error; err != nil {
					return err
				}
			}
		}
		if !hasCore {
			return nil
		}
		var drafts []model.InboundDraft
		if err := tx.WithContext(ctx).
			Where("source = ? AND status IN ?", LegacySelfStealSource,
				[]string{inbounddrafts.StatusBlocked, inbounddrafts.StatusReviewRequired}).
			Order("id ASC").Find(&drafts).Error; err != nil {
			return err
		}
		for _, draft := range drafts {
			if err := ctx.Err(); err != nil {
				return err
			}
			inspection, decodeErr := DecodeLegacySelfStealPayload(draft.Payload)
			notes, err := json.Marshal(struct {
				Classification string   `json:"classification"`
				ReasonCodes    []string `json:"reasonCodes"`
			}{Classification: inspection.Classification, ReasonCodes: inspection.ReasonCodes})
			if err != nil {
				return err
			}
			if decodeErr != nil && len(inspection.ReasonCodes) == 0 {
				return errors.New(LegacySelfStealInvalidReason)
			}
			if err := tx.WithContext(ctx).Model(&model.InboundDraft{}).
				Where("id = ? AND source = ? AND status IN ?", draft.Id, LegacySelfStealSource,
					[]string{inbounddrafts.StatusBlocked, inbounddrafts.StatusReviewRequired}).
				Updates(map[string]any{
					"status":       inbounddrafts.StatusDiscarded,
					"review_notes": notes,
					"updated_at":   now.Unix(),
				}).Error; err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}
