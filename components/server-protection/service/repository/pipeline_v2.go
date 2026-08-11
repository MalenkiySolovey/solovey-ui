package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/graylist"
	"gorm.io/gorm"
)

// ProcessAdmittedSignalV2 persists and evaluates one already-admitted signal
// atomically. It has no executor, reservation, topology, kernel, or proxy
// dependency and can persist only NOT_APPLIED policy artifacts.
func (r *Repository) ProcessAdmittedSignalV2(ctx context.Context, input graylist.PipelineInput, accepted graylist.AcceptedSignal) (graylist.PipelineResult, error) {
	if r == nil || r.db == nil {
		return graylist.PipelineResult{}, errors.New("server-protection repository is not initialized")
	}
	if accepted.Signal.SignalID == "" || accepted.Signal.SignalID != input.Signal.SignalID {
		return graylist.PipelineResult{}, errors.New("admitted signal does not match pipeline input")
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
		input.Now = now
	}
	if input.Policy.Revision == "" {
		input.Policy = graylist.DefaultPolicyV2()
	}
	if err := accepted.Signal.Validate(now); err != nil {
		return graylist.PipelineResult{}, err
	}

	var output graylist.PipelineResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := saveSignalTx(tx, accepted.Signal); err != nil {
			return err
		}

		identity := domain.GraylistStateV2{
			Subject: accepted.Signal.Subject, ResourceID: accepted.Signal.Scope.TargetResourceID,
			EndpointID: accepted.Signal.Scope.EndpointID, Transport: accepted.Signal.Scope.Transport,
			PolicyRevision: input.Policy.Revision, StrategyRevision: input.StrategyRevision,
			CapabilityRevision: input.CapabilityRevision,
		}
		identity.FinalizeID()
		expectedRevision := uint64(0)
		var row GraylistStateV2Model
		loadErr := tx.Where("state_id = ?", identity.StateID).First(&row).Error
		switch {
		case loadErr == nil:
			existing, err := graylistStateDomain(row)
			if err != nil {
				return err
			}
			input.Existing = &existing
			expectedRevision = existing.Revision
		case errors.Is(loadErr, gorm.ErrRecordNotFound):
			input.Existing = nil
		default:
			return loadErr
		}

		result, err := graylist.ProcessAccepted(input, accepted)
		if err != nil {
			return err
		}
		if result.State.ActualActionState != "NOT_APPLIED" || result.State.AppliedActionRefID != "" {
			return errors.New("graylist pipeline attempted to persist applied state")
		}
		if result.Changed {
			payload, err := json.Marshal(result.State)
			if err != nil {
				return err
			}
			if _, err := storeGraylistEvaluationTx(tx, result.State, expectedRevision, graylistStateModel(result.State, payload), payload); err != nil {
				return err
			}
		}
		if result.Decision.DecisionID != "" {
			if err := createDecisionIdempotentTx(tx, result.Decision); err != nil {
				return err
			}
		}
		if result.Resolution.PlannedResponse != nil {
			if err := createPlannedResponseIdempotentTx(tx, *result.Resolution.PlannedResponse); err != nil {
				return err
			}
		}
		globalLimit, perTargetLimit := contractRetentionLimits(tx)
		if err := pruneSignalContracts(tx, globalLimit, perTargetLimit, accepted.Signal.Scope.TargetResourceID); err != nil {
			return err
		}
		if err := pruneDecisionContracts(tx, globalLimit); err != nil {
			return err
		}
		if err := prunePlannedResponseContracts(tx, globalLimit); err != nil {
			return err
		}
		output = result
		return nil
	})
	return output, err
}

func createDecisionIdempotentTx(tx *gorm.DB, decision domain.ProtectionDecisionV2) error {
	if err := decision.Validate(decision.CreatedAt); err != nil {
		return err
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	model := ProtectionDecisionV2Model{
		DecisionID: decision.DecisionID, Schema: decision.Schema, PolicyRevision: decision.PolicyRevision,
		SubjectType: decision.Subject.Type, SubjectValue: decision.Subject.Value, Scope: string(decision.Scope.Scope),
		RequestedIntent: string(decision.RequestedIntent), ResolvedIntent: string(decision.CapabilityResolution.ResolvedIntent),
		ActionImplemented: decision.CapabilityResolution.Implemented, State: string(decision.State),
		CreatedAt: decision.CreatedAt.Unix(), ExpiresAt: decision.ExpiresAt.Unix(), ContractJSON: payload,
	}
	var existing ProtectionDecisionV2Model
	loadErr := tx.Where("decision_id = ?", model.DecisionID).First(&existing).Error
	if loadErr == nil {
		if bytes.Equal(existing.ContractJSON, payload) {
			return nil
		}
		return errors.New("conflicting decision replay")
	}
	if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return loadErr
	}
	return tx.Create(&model).Error
}

func createPlannedResponseIdempotentTx(tx *gorm.DB, response domain.PlannedResponseV2) error {
	if err := response.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	model := PlannedResponseV2Model{
		ResponseID: response.ResponseID, DecisionID: response.DecisionID, ResourceID: response.ResourceID,
		EndpointID: response.EndpointID, SelectedIntent: string(response.SelectedIntent),
		CapabilityRevision: response.CapabilityRevision, ActualState: response.ActualState,
		CreatedAt: response.CreatedAt.Unix(), ExpiresAt: response.ExpiresAt.Unix(), ContractJSON: payload,
	}
	var existing PlannedResponseV2Model
	loadErr := tx.Where("response_id = ?", model.ResponseID).First(&existing).Error
	if loadErr == nil {
		if bytes.Equal(existing.ContractJSON, payload) {
			return nil
		}
		return errors.New("conflicting planned response replay")
	}
	if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return loadErr
	}
	return tx.Create(&model).Error
}

func prunePlannedResponseContracts(tx *gorm.DB, globalLimit int) error {
	excess := tx.Model(&PlannedResponseV2Model{}).Select("id").Order("created_at DESC, id DESC").Offset(max(globalLimit, 1)).Limit(-1)
	return tx.Where("id IN (?)", excess).Delete(&PlannedResponseV2Model{}).Error
}
