package service

import (
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/realtime"

	"gorm.io/gorm"
)

type configSavePlan struct {
	singboxapply.Plan
}

type ComponentConfigChangeEffects struct {
	PrimaryObject  string
	IncludeObjects []string
	CoreRestart    bool
	RestartReason  string
}

var invalidateSubscriptionOutputCacheAfterSave func()

func RegisterSubscriptionOutputCacheInvalidator(fn func()) {
	invalidateSubscriptionOutputCacheAfterSave = fn
}

func newConfigSavePlan(primaryObject string) configSavePlan {
	return configSavePlan{Plan: singboxapply.NewPlan(primaryObject)}
}

func (p *configSavePlan) IncludeSaveObjects(objects ...singboxapply.Object) {
	p.Plan.IncludeSaveObjects(objects...)
}

func (s *ConfigService) recordConfigChange(tx *gorm.DB, loginUser string, obj string, act string, data json.RawMessage) error {
	return tx.Create(&model.Changes{
		DateTime: time.Now().Unix(),
		Actor:    loginUser,
		Key:      obj,
		Action:   act,
		Obj:      redactChangePayload(data),
	}).Error
}

func (s *ConfigService) RecordComponentConfigChange(tx *gorm.DB, loginUser string, obj string, act string, payload any) error {
	data, err := marshalConfigChangePayload(payload)
	if err != nil {
		return err
	}
	return s.recordConfigChange(tx, loginUser, obj, act, data)
}

func marshalConfigChangePayload(payload any) (json.RawMessage, error) {
	switch value := payload.(type) {
	case nil:
		return json.RawMessage("null"), nil
	case json.RawMessage:
		return value, nil
	case []byte:
		return json.RawMessage(value), nil
	default:
		return json.Marshal(value)
	}
}

func (s *ConfigService) ApplyComponentConfigChangeEffects(effects ComponentConfigChangeEffects) {
	s.setLastUpdate(time.Now().Unix())
	if !effects.CoreRestart {
		realtime.Publish(realtime.TopicConfigInvalidated, nil)
		return
	}
	primaryObject := effects.PrimaryObject
	if primaryObject == "" {
		primaryObject = "component"
	}
	plan := newConfigSavePlan(primaryObject)
	plan.IncludeObjects(effects.IncludeObjects...)
	plan.RequireCoreRestart(effects.RestartReason)
	s.applyConfigSaveEffects(plan, nil)
}

func (s *ConfigService) applyConfigSaveEffects(plan configSavePlan, afterCommitEffects []ConfigSaveAfterCommit) {
	for _, effect := range afterCommitEffects {
		if effect != nil {
			effect()
		}
	}
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	if invalidateSubscriptionOutputCacheAfterSave != nil {
		invalidateSubscriptionOutputCacheAfterSave()
	}
	s.applyCoreSaveEffect(plan)
}

func (s *ConfigService) applyCoreSaveEffect(plan configSavePlan) {
	if s.coreInstance() == nil {
		return
	}
	manager := s.runtime().restart()
	if manager == nil {
		logger.Warning("sing-box post-save sync skipped: restart manager not initialized")
		return
	}
	_ = manager.RunBlocking(func() error {
		s.applyCoreSaveEffectLocked(plan)
		return nil
	})
}

func (s *ConfigService) applyCoreSaveEffectLocked(plan configSavePlan) {
	coreInstance := s.coreInstance()
	if coreInstance == nil {
		return
	}
	lifecycle := s.configCoreLifecycle()
	if plan.RequiresCoreRestart() {
		if reason := plan.RestartReason(); reason != "" {
			logger.Info("sing-box full restart after save: ", reason)
		}
		if coreInstance.IsRunning() {
			if restartErr := lifecycle.restartCoreLocked(); restartErr != nil {
				logger.Warning("sing-box restart after save failed: ", restartErr)
			}
		} else {
			if startErr := lifecycle.startCoreLocked(true); startErr != nil {
				logger.Warning("sing-box start after save failed: ", startErr)
			}
		}
		return
	}
	if !coreInstance.IsRunning() {
		if startErr := lifecycle.startCoreLocked(true); startErr != nil {
			logger.Warning("sing-box start after save failed: ", startErr)
		}
		return
	}
	if !plan.HasObjectChanges() {
		return
	}
	if err := s.applyObjectChangesLocked(plan); err == nil {
		return
	} else {
		logger.Warning("sing-box partial reload after save failed: ", err)
	}
	if restartErr := lifecycle.restartCoreLocked(); restartErr != nil {
		logger.Error("sing-box restart after failed partial reload also failed; core may be out of sync: ", restartErr)
	}
}

func (s *ConfigService) applyObjectChangesLocked(plan configSavePlan) error {
	return singboxapply.ExecuteObjectChanges(configDatabase(), plan.Plan, s.configCoreObjectApplier())
}
