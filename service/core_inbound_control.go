package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/gorm"
)

// CoreInboundControl returns the core-owned narrow fallback adapter. It is a
// service contract for later orchestration; it does not expose an HTTP route.
func (s *ConfigService) CoreInboundControl() *coreinboundcontrol.Service {
	if s == nil {
		return nil
	}
	s.coreInboundControlMu.Lock()
	defer s.coreInboundControlMu.Unlock()
	if s.coreInboundControl != nil {
		return s.coreInboundControl
	}
	var effective coreinboundcontrol.EffectiveInboundReader
	if coreInstance := s.coreInstance(); coreInstance != nil {
		effective = coreInstance
	}
	s.coreInboundControl = coreinboundcontrol.NewWithMutations(configDatabase(), effective, coreinboundcontrol.MutationDependencies{
		Coordinator: configCoreMutationCoordinator{runtime: s.runtime()},
		Runtime:     configCoreMutationRuntime{service: s},
		Hooks:       configCoreMutationHooks{service: s},
		Hydrator:    configCoreCandidateHydrator{service: s},
	})
	return s.coreInboundControl
}

type configCoreCandidateHydrator struct {
	service *ConfigService
}

func (h configCoreCandidateHydrator) HydrateInbound(ctx context.Context, tx *gorm.DB, inbound *model.Inbound, content []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.service == nil || inbound == nil {
		return nil, fmt.Errorf("candidate hydrator unavailable")
	}
	return h.service.InboundService.AddUsers(tx, content, inbound.Id, inbound.Type)
}

type configCoreMutationCoordinator struct {
	runtime *Runtime
}

func (c configCoreMutationCoordinator) RunBlockingContext(ctx context.Context, operation func() error) error {
	if c.runtime == nil || c.runtime.restart() == nil {
		return fmt.Errorf("core mutation coordinator unavailable")
	}
	return c.runtime.restart().RunBlockingContext(ctx, operation)
}

type configCoreMutationRuntime struct {
	service *ConfigService
}

func (r configCoreMutationRuntime) ApplyInbound(ctx context.Context, inboundID uint) (coreinboundcontrol.RuntimeInboundObservationV1, error) {
	if err := ctx.Err(); err != nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, err
	}
	if r.service == nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, fmt.Errorf("config service unavailable")
	}
	coreInstance := r.service.coreInstance()
	if coreInstance == nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, fmt.Errorf("core unavailable")
	}
	if !coreInstance.IsRunning() {
		if err := r.service.startCoreLocked(true); err != nil {
			return coreinboundcontrol.RuntimeInboundObservationV1{}, err
		}
	} else {
		plan := configSavePlan{Plan: singboxapply.NewPlan(singboxapply.ObjectInbounds.String())}
		plan.MergeInboundChange(&singboxapply.Change{ReloadIDs: []uint{inboundID}})
		if err := r.service.applyObjectChangesLocked(plan); err != nil {
			restartErr := r.service.restartCoreLocked()
			if restartErr != nil {
				return coreinboundcontrol.RuntimeInboundObservationV1{}, fmt.Errorf("partial reload and restart failed")
			}
			return coreinboundcontrol.RuntimeInboundObservationV1{}, fmt.Errorf("partial reload failed")
		}
	}
	if err := ctx.Err(); err != nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, err
	}
	var inbound model.Inbound
	if err := configDatabase().WithContext(ctx).Select("id", "tag").First(&inbound, inboundID).Error; err != nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, err
	}
	return observeCoreInbound(coreInstance, inbound.Tag), nil
}

func (r configCoreMutationRuntime) ObserveInbound(ctx context.Context, tag string) (coreinboundcontrol.RuntimeInboundObservationV1, error) {
	if err := ctx.Err(); err != nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, err
	}
	if r.service == nil || r.service.coreInstance() == nil {
		return coreinboundcontrol.RuntimeInboundObservationV1{}, fmt.Errorf("core unavailable")
	}
	return observeCoreInbound(r.service.coreInstance(), tag), nil
}

func observeCoreInbound(coreInstance *coreruntime.Core, tag string) coreinboundcontrol.RuntimeInboundObservationV1 {
	available, records := coreInstance.LookupInboundRuntime(tag)
	observation := coreinboundcontrol.RuntimeInboundObservationV1{
		RuntimeAvailable: available, MatchingInboundCount: len(records),
	}
	if len(records) == 1 {
		observation.Tag = records[0].Tag
		observation.Type = records[0].Type
		observation.OptionsDigest = records[0].OptionsDigest
		observation.ManagerGeneration = records[0].ManagerGeneration
	}
	return observation
}

type configCoreMutationHooks struct {
	service *ConfigService
}

func (h configCoreMutationHooks) BeforeCommit(ctx context.Context, tx *gorm.DB, inbound *model.Inbound, variant coreinboundcontrol.FallbackPatchVariantV1, changed []coreinboundcontrol.ChangedFieldV1) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.service == nil || inbound == nil {
		return fmt.Errorf("core mutation hooks unavailable")
	}
	if err := entityinbounds.FillOutboundJSON(inbound, ""); err != nil {
		return err
	}
	if err := tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("out_json", inbound.OutJson).Error; err != nil {
		return err
	}
	if err := h.service.ClientService.UpdateLinksByInboundChange(tx, &[]model.Inbound{*inbound}, "", ""); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Schema    string                                    `json:"schema"`
		Variant   coreinboundcontrol.FallbackPatchVariantV1 `json:"variant"`
		InboundID uint                                      `json:"inboundId"`
		Changed   []coreinboundcontrol.ChangedFieldV1       `json:"changed"`
	}{coreinboundcontrol.FallbackMutationSchemaV1, variant, inbound.Id, changed})
	if err != nil {
		return err
	}
	return h.service.recordConfigChange(tx, "system", singboxapply.ObjectInbounds.String(), "fallback_patch", payload)
}

func (h configCoreMutationHooks) AfterCommit(coreinboundcontrol.FallbackPatchVariantV1, uint) {
	if h.service == nil {
		return
	}
	h.service.setLastUpdate(time.Now().Unix())
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	invalidateSubscriptionOutputCache()
}
