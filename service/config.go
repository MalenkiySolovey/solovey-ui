package service

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core"
	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

type ConfigService struct {
	ClientService
	TlsService
	SettingService
	InboundService
	OutboundService
	ServicesService
	EndpointService
	Runtime *Runtime
}

type SingBoxConfig struct {
	Log          json.RawMessage   `json:"log"`
	Dns          json.RawMessage   `json:"dns"`
	Ntp          json.RawMessage   `json:"ntp"`
	Inbounds     []json.RawMessage `json:"inbounds"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Services     []json.RawMessage `json:"services"`
	Endpoints    []json.RawMessage `json:"endpoints"`
	Route        json.RawMessage   `json:"route"`
	Experimental json.RawMessage   `json:"experimental"`
}

func NewConfigService(core *core.Core) *ConfigService {
	runtime := NewRuntime(core)
	SetDefaultRuntime(runtime)
	return NewConfigServiceWithRuntime(runtime)
}

func NewConfigServiceWithRuntime(runtime *Runtime) *ConfigService {
	runtime = runtimeOrDefault(runtime)
	return &ConfigService{
		ClientService:   ClientService{Runtime: runtime},
		TlsService:      TlsService{Runtime: runtime, InboundService: InboundService{Runtime: runtime, ClientService: ClientService{Runtime: runtime}}, ServicesService: ServicesService{Runtime: runtime}},
		SettingService:  SettingService{},
		InboundService:  InboundService{Runtime: runtime, ClientService: ClientService{Runtime: runtime}},
		OutboundService: OutboundService{},
		ServicesService: ServicesService{Runtime: runtime},
		EndpointService: EndpointService{},
		Runtime:         runtime,
	}
}

func (s *ConfigService) GetConfig(data string) (*[]byte, error) {
	rawConfig, err := s.singBoxConfigBuilder().Build(data)
	if err != nil {
		return nil, err
	}
	return &rawConfig, nil
}

func (s *ConfigService) singBoxConfigBuilder() SingBoxConfigBuilder {
	if s == nil {
		return NewSingBoxConfigBuilder(DefaultRuntime())
	}
	return SingBoxConfigBuilder{
		SettingService:  s.SettingService,
		InboundService:  s.InboundService,
		OutboundService: s.OutboundService,
		ServicesService: s.ServicesService,
		EndpointService: s.EndpointService,
	}
}

// startCore starts sing-box. When force is true, the cool-down between failed
// starts is bypassed, which is required for user-initiated restarts so the API
// reflects the real start status instead of silently succeeding.
func (s *ConfigService) startCore(force bool) error {
	manager := s.runtime().restart()
	if manager == nil {
		return common.NewError("restart manager not initialized")
	}
	return manager.run(func() error {
		return s.startCoreLocked(force)
	})
}

func (s *ConfigService) startCoreLocked(force bool) error {
	coreInstance := s.coreInstance()
	if coreInstance == nil {
		return common.NewError("core not initialized")
	}
	if coreInstance.IsRunning() {
		return nil
	}
	runtime := s.runtime()
	if !force && runtime.startCooldownActive() {
		logger.Info("start core cooldown ", runtime.coreStartCooldownDuration()/time.Second, " seconds")
		return nil
	}

	logger.Info("starting core")
	rawConfig, err := s.GetConfig("")
	if err != nil {
		return err
	}
	err = coreInstance.Start(*rawConfig)
	if err != nil {
		runtime.markCoreStartFailed()
		logger.Error("start sing-box err:", err.Error())
		return err
	}
	runtime.markCoreStartSucceeded()
	logger.Info("sing-box started")
	return nil
}

// StartCore is the cron-friendly variant: it respects the cooldown so a
// failing core does not get hammered every 5 seconds.
func (s *ConfigService) StartCore() error {
	return s.startCore(false)
}

// RestartCore is invoked from user actions; it bypasses the cooldown so the
// caller observes the true start status.
func (s *ConfigService) RestartCore() error {
	manager := s.runtime().restart()
	if manager == nil {
		return common.NewError("restart manager not initialized")
	}
	return manager.run(func() error {
		coreInstance := s.coreInstance()
		if coreInstance == nil {
			return common.NewError("core not initialized")
		}
		if err := s.StopCore(); err != nil {
			return err
		}
		return s.startCoreLocked(true)
	})
}

func (s *ConfigService) StopCore() error {
	coreInstance := s.coreInstance()
	if coreInstance == nil {
		return common.NewError("core not initialized")
	}
	err := coreInstance.Stop()
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped")
	return nil
}

func (s *ConfigService) IsCoreRunning() bool {
	coreInstance := s.coreInstance()
	return coreInstance != nil && coreInstance.IsRunning()
}

func (s *ConfigService) CheckOutbound(tag string, link string) core.CheckOutboundResult {
	if tag == "" {
		return core.CheckOutboundResult{Error: "missing query parameter: tag"}
	}
	coreInstance := s.coreInstance()
	if coreInstance == nil || !coreInstance.IsRunning() {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	return coreInstance.CheckOutbound(coreInstance.GetCtx(), tag, link)
}

func (s *ConfigService) CheckOutboundWithContext(ctx context.Context, tag string, link string) core.CheckOutboundResult {
	if tag == "" {
		return core.CheckOutboundResult{Error: "missing query parameter: tag"}
	}
	coreInstance := s.coreInstance()
	if coreInstance == nil || !coreInstance.IsRunning() {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	return coreInstance.CheckOutbound(ctx, tag, link)
}

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, loginUser string, hostname string) (objs []string, err error) {
	plan := newConfigSavePlan(obj)
	auditTelegramBackupPassphrase, auditTelegramBackupPassphraseConfigured, err := s.telegramBackupPassphraseAuditState(obj, data)
	if err != nil {
		return nil, err
	}

	db := database.GetDB()
	tx := db.Begin()
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	if err = s.applyConfigSaveMutation(tx, &plan, obj, act, data, initUsers, hostname); err != nil {
		return nil, err
	}
	if err = s.recordConfigChange(tx, loginUser, obj, act, data); err != nil {
		return nil, err
	}

	s.setLastUpdate(time.Now().Unix())

	if err = tx.Commit().Error; err != nil {
		return plan.Objects(), err
	}
	committed = true

	s.applyConfigSaveEffects(plan, loginUser, auditTelegramBackupPassphrase, auditTelegramBackupPassphraseConfigured)

	return plan.Objects(), nil
}

func (s *ConfigService) coreInstance() *core.Core {
	if s == nil {
		return DefaultRuntime().Core()
	}
	return s.runtime().Core()
}

func (s *ConfigService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *ConfigService) telegramBackupPassphraseAuditState(obj string, data json.RawMessage) (bool, bool, error) {
	if obj != "settings" {
		return false, false, nil
	}
	var settings map[string]string
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, false, err
	}
	newPassphrase, ok := settings[settingKeyTelegramBackupPassphrase]
	if !ok || newPassphrase == StoredSecretMarker {
		return false, false, nil
	}
	oldPassphrase, err := s.SettingService.GetTelegramBackupPassphraseBytes()
	if err != nil {
		return false, false, err
	}
	defer zeroBytes(oldPassphrase)
	if string(oldPassphrase) == newPassphrase {
		return false, false, nil
	}
	return true, newPassphrase != "", nil
}

func redactChangePayload(data json.RawMessage) json.RawMessage {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		encoded, marshalErr := json.Marshal(redact.String(string(data)))
		if marshalErr != nil {
			return json.RawMessage(`"[REDACTED]"`)
		}
		return encoded
	}
	encoded, err := json.Marshal(redact.Value(payload))
	if err != nil {
		return json.RawMessage(`"[REDACTED]"`)
	}
	return encoded
}

func (s *ConfigService) CheckChanges(lu string) (bool, error) {
	if lu == "" {
		return true, nil
	}
	lastUpdate := s.getLastUpdate()
	if lastUpdate == 0 {
		db := database.GetDB()
		var count int64
		intLu, err := strconv.ParseInt(lu, 10, 64)
		if err != nil {
			return false, err
		}
		err = db.Model(model.Changes{}).Where("date_time > ?", intLu).Count(&count).Error
		if err == nil {
			s.setLastUpdate(time.Now().Unix())
		}
		return count > 0, err
	}
	intLu, err := strconv.ParseInt(lu, 10, 64)
	return lastUpdate > intLu, err
}

func (s *ConfigService) GetChanges(actor string, chngKey string, count string) []model.Changes {
	c, _ := strconv.Atoi(count)
	if c <= 0 || c > 200 {
		c = 20
	}
	db := database.GetDB().Model(model.Changes{})
	if len(actor) > 0 {
		db = db.Where("actor = ?", actor)
	}
	if len(chngKey) > 0 {
		db = db.Where("key = ?", chngKey)
	}
	var chngs []model.Changes
	err := db.Order("id desc").Limit(c).Scan(&chngs).Error
	if err != nil {
		logger.Warning(err)
	}
	return chngs
}

func (s *ConfigService) setLastUpdate(value int64) {
	s.runtime().updates().Set(value)
}

func (s *ConfigService) getLastUpdate() int64 {
	return s.runtime().updates().Get()
}
