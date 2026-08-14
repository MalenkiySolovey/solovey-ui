package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

var subscriptionOutputCacheInvalidator = struct {
	sync.RWMutex
	fn func()
}{}

func RegisterSubscriptionOutputCacheInvalidator(fn func()) {
	subscriptionOutputCacheInvalidator.Lock()
	subscriptionOutputCacheInvalidator.fn = fn
	subscriptionOutputCacheInvalidator.Unlock()
}

func invalidateSubscriptionOutputCache() {
	subscriptionOutputCacheInvalidator.RLock()
	invalidate := subscriptionOutputCacheInvalidator.fn
	subscriptionOutputCacheInvalidator.RUnlock()
	if invalidate != nil {
		invalidate()
	}
}

// InvalidateSubscriptionCaches discards derived public subscription output
// after a committed mutation that does not use the config-save transaction.
func InvalidateSubscriptionCaches() {
	invalidateSubscriptionOutputCache()
}

func newConfigSavePlan(primaryObject string) configSavePlan {
	return configSavePlan{Plan: singboxapply.NewPlan(primaryObject)}
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
	if err := s.applyConfigSaveEffects(plan, nil); err != nil {
		logger.Error("sing-box component configuration sync failed: ", err)
	}
}

func (s *ConfigService) applyConfigSaveEffects(plan configSavePlan, afterCommitEffects []ConfigSaveAfterCommit) error {
	for _, effect := range afterCommitEffects {
		if effect != nil {
			effect()
		}
	}
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	InvalidateSubscriptionCaches()
	return s.applyCoreSaveEffect(plan)
}

func (s *ConfigService) applyCoreSaveEffect(plan configSavePlan) error {
	if s.coreInstance() == nil {
		return nil
	}
	manager := s.runtime().restart()
	if manager == nil {
		return errors.New("sing-box post-save sync is unavailable: restart manager is not initialized")
	}
	return manager.RunBlocking(func() error {
		return s.applyCoreSaveEffectLocked(plan)
	})
}

func (s *ConfigService) applyCoreSaveEffectLocked(plan configSavePlan) error {
	coreInstance := s.coreInstance()
	if coreInstance == nil {
		return nil
	}
	lifecycle := s.configCoreLifecycle()
	if plan.RequiresCoreRestart() {
		if reason := plan.RestartReason(); reason != "" {
			logger.Info("sing-box full restart after save: ", reason)
		}
		if coreInstance.IsRunning() {
			if restartErr := lifecycle.restartCoreLocked(); restartErr != nil {
				return fmt.Errorf("restart sing-box after committed configuration change: %w", restartErr)
			}
		} else {
			if startErr := lifecycle.startCoreLocked(true); startErr != nil {
				return fmt.Errorf("start sing-box after committed configuration change: %w", startErr)
			}
		}
		return nil
	}
	if !coreInstance.IsRunning() {
		if startErr := lifecycle.startCoreLocked(true); startErr != nil {
			return fmt.Errorf("start sing-box after committed configuration change: %w", startErr)
		}
		return nil
	}
	if !plan.HasObjectChanges() {
		return nil
	}
	if err := s.applyObjectChangesLocked(plan); err == nil {
		return nil
	} else {
		logger.Warning("sing-box partial reload after save failed: ", err)
		if restartErr := lifecycle.restartCoreLocked(); restartErr != nil {
			return errors.Join(fmt.Errorf("apply committed sing-box object changes: %w", err),
				fmt.Errorf("restart sing-box after failed partial reload: %w", restartErr))
		}
	}
	return nil
}

func (s *ConfigService) applyObjectChangesLocked(plan configSavePlan) error {
	return singboxapply.ExecuteObjectChanges(configDatabase(), plan.Plan, s.configCoreObjectApplier())
}
