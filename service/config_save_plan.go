package service

import (
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/realtime"

	"gorm.io/gorm"
)

type configSavePlan struct {
	objects           []string
	requiresCoreReset bool
}

func newConfigSavePlan(primaryObject string) configSavePlan {
	return configSavePlan{objects: []string{primaryObject}}
}

func (p *configSavePlan) IncludeObjects(objects ...string) {
	p.objects = append(p.objects, objects...)
}

func (p *configSavePlan) IncludeSaveObjects(objects ...configSaveObject) {
	for _, object := range objects {
		p.IncludeObjects(object.String())
	}
}

func (p *configSavePlan) RequireCoreRestart() {
	p.requiresCoreReset = true
}

func (p configSavePlan) Objects() []string {
	return append([]string(nil), p.objects...)
}

func (p configSavePlan) RequiresCoreRestart() bool {
	return p.requiresCoreReset
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

func (s *ConfigService) applyConfigSaveEffects(plan configSavePlan, loginUser string, auditTelegramBackupPassphrase bool, auditTelegramBackupPassphraseConfigured bool) {
	if auditTelegramBackupPassphrase {
		s.SettingService.recordTelegramBackupPassphraseChanged(loginUser, auditTelegramBackupPassphraseConfigured)
	}
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	s.applyCoreSaveEffect(plan)
}

func (s *ConfigService) applyCoreSaveEffect(plan configSavePlan) {
	coreInstance := s.coreInstance()
	if coreInstance == nil {
		return
	}
	if plan.RequiresCoreRestart() {
		if coreInstance.IsRunning() {
			if restartErr := s.RestartCore(); restartErr != nil {
				logger.Warning("sing-box restart after save failed: ", restartErr)
			}
		} else {
			if startErr := s.startCore(true); startErr != nil {
				logger.Warning("sing-box start after save failed: ", startErr)
			}
		}
	} else if !coreInstance.IsRunning() {
		if startErr := s.startCore(true); startErr != nil {
			logger.Warning("sing-box start after save failed: ", startErr)
		}
	}
}
