package app

import (
	"context"
	"log"
	"os"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/cronjob/scheduler"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	paidcore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	paidtelegram "github.com/MalenkiySolovey/solovey-ui/paidsub/telegram"
	"github.com/MalenkiySolovey/solovey-ui/service"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/MalenkiySolovey/solovey-ui/sub"
	"github.com/MalenkiySolovey/solovey-ui/web"
)

type APP struct {
	service.SettingService
	configService *service.ConfigService
	webServer     *web.Server
	subServer     *sub.Server
	cronScheduler *scheduler.Scheduler
	core          *coreruntime.Core
	runtime       *service.Runtime
}

func NewApp() *APP {
	return &APP{}
}

func (a *APP) Init() error {
	log.Printf("%v %v", configidentity.GetName(), configidentity.GetVersion())

	a.initLog()

	if executable, err := os.Executable(); err == nil && executable != "" && serviceupdate.CheckPending(executable) {
		logger.Warning("self-update failed to boot twice; restored previous binary")
		os.Exit(1)
	}

	// Run schema migrations against the on-disk DB before opening it. This
	// turns the upgrade flow into a one-step procedure: drop in the new
	// binary, restart, and the panel adapts the legacy schema in place. The
	// run is a no-op if the database is already at the current version or if
	// it does not yet exist (first install).
	if err := migration.MigrateDb(); err != nil {
		return err
	}

	err := dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}

	// Init Setting
	if _, err := a.SettingService.GetAllSetting(); err != nil {
		logger.Warning("failed to initialize settings: ", err)
	}

	// Re-seal any secret settings still encrypted under a DB-derived key once an
	// out-of-database SUI_SECRETBOX_KEY is configured. No-op without the env key,
	// idempotent, and fail-safe per row; a failure here must not block startup.
	if n, err := a.SettingService.ResealSecretSettings(); err != nil {
		logger.Warning("failed to re-seal secret settings: ", err)
	} else if n > 0 {
		logger.Info("re-sealed ", n, " secret setting(s) under SUI_SECRETBOX_KEY")
	}
	if err := ipmonitor.WarmUp(); err != nil {
		return err
	}

	a.core = coreruntime.NewCore(ipmonitor.Observer{})
	a.runtime = service.NewRuntime(a.core)
	service.SetDefaultRuntime(a.runtime)

	// Mirror ipmonitor IP-limit enforcement into the durable audit log (D-5).
	// Set via a hook to avoid an import cycle; debounced upstream so it cannot
	// flood the audit log.
	ipmonitor.SecurityEventAuditHook = func(clientName string, kind string, payload map[string]any) {
		_ = (&service.AuditService{}).Record(service.AuditEvent{
			Actor:    "system",
			Event:    "ip_limit_enforced",
			Resource: "ipmonitor",
			Severity: service.AuditSeverityWarn,
			Details:  payload,
		})
	}

	// Subscription server hooks: connect audit and rate-limit settings to the
	// service layer without the pure subscription-server package importing it.
	subserver.ListenFallbackAuditHook = func(component, requestedAddr, fallbackAddr string, bindErr error) {
		_ = (&service.AuditService{}).RecordListenFallback(component, requestedAddr, fallbackAddr, bindErr)
	}
	subserver.SubEnumerationAuditHook = func(ip string, invalidLookups, windowMinutes int) {
		_ = (&service.AuditService{}).Record(service.AuditEvent{
			Actor:    "anonymous",
			Event:    "sub_enumeration",
			Resource: "sub",
			Severity: service.AuditSeverityWarn,
			IP:       ip,
			Details:  map[string]any{"invalidLookups": invalidLookups, "windowMinutes": windowMinutes},
		})
	}
	subserver.SubRateLimitProvider = func() (int, error) {
		return (&service.SettingService{}).GetSubRateLimitPerIP()
	}

	a.cronScheduler = scheduler.New()
	a.webServer, err = web.NewServer(web.WithRuntime(a.runtime))
	if err != nil {
		return err
	}
	a.subServer = sub.NewServer()

	a.configService = service.NewConfigServiceWithRuntime(a.runtime)

	// Experimental Paid Subscriptions module owns its own schema; create it
	// idempotently at startup. Non-fatal: a failure here must not block core.
	if err := paidcore.EnsureSchema(dbsqlite.DB()); err != nil {
		logger.Warning("failed to ensure paidsub schema: ", err)
	}

	return nil
}

func (a *APP) Start() error {
	loc, err := a.SettingService.GetTimeLocation()
	if err != nil {
		return err
	}

	trafficAge, err := a.SettingService.GetTrafficAge()
	if err != nil {
		return err
	}

	err = a.cronScheduler.Start(loc, trafficAge)
	if err != nil {
		return err
	}

	err = a.webServer.Start()
	if err != nil {
		return err
	}

	err = a.subServer.Start()
	if err != nil {
		return err
	}

	// Experimental Paid Subscriptions client bot. Self-gates on paidSubEnabled
	// internally, so starting unconditionally is safe and lets the admin toggle
	// it at runtime without a restart.
	paidtelegram.StartBot()
	service.StartRemoteOutboundAutoRefresh(a.runtime)

	// A core start failure is intentionally non-fatal: the web/sub panel must
	// stay up so the admin can fix a bad sing-box config through the UI. The
	// failure is surfaced loudly here and reflected in the panel's core status.
	if err = a.configService.StartCore(); err != nil {
		logger.Error("sing-box core failed to start; panel stays up so you can fix the config: ", err)
	}
	if executable, err := os.Executable(); err == nil && executable != "" {
		serviceupdate.ClearPending(executable)
	}

	return nil
}

func (a *APP) Stop() {
	service.StopRestartManager()
	a.cronScheduler.Stop()
	err := a.subServer.Stop()
	if err != nil {
		logger.Warning("stop Sub Server err:", err)
	}
	err = a.webServer.Stop()
	if err != nil {
		logger.Warning("stop Web Server err:", err)
	}
	err = a.configService.StopCore()
	if err != nil {
		logger.Warning("stop Core err:", err)
	}
	tokenCtx, tokenCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tokenCancel()
	if err := service.StopTokenUseDebouncer(tokenCtx); err != nil {
		logger.Warning("stop token use debouncer err:", err)
	}
	telegramCtx, telegramCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer telegramCancel()
	if err := service.StopTelegramNotifier(telegramCtx); err != nil {
		logger.Warning("stop telegram notifier err:", err)
	}
	remoteSubCtx, remoteSubCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer remoteSubCancel()
	if err := service.StopRemoteOutboundAutoRefresh(remoteSubCtx); err != nil {
		logger.Warning("stop remote subscription auto refresh err:", err)
	}
	paidSubCtx, paidSubCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer paidSubCancel()
	if err := paidtelegram.StopBot(paidSubCtx); err != nil {
		logger.Warning("stop paidsub bot err:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.StopAuditWriter(ctx); err != nil {
		logger.Warning("stop audit writer err:", err)
	}
}

func (a *APP) initLog() {
	switch configlogging.GetLogLevel() {
	case configlogging.Debug:
		logger.Init(logger.LevelDebug)
	case configlogging.Info:
		logger.Init(logger.LevelInfo)
	case configlogging.Warn:
		logger.Init(logger.LevelWarning)
	case configlogging.Error:
		logger.Init(logger.LevelError)
	default:
		logger.Init(logger.LevelInfo)
	}
}

func (a *APP) RestartApp() {
	a.Stop()
	if err := a.Start(); err != nil {
		logger.Warning("failed to restart app: ", err)
	}
}

func (a *APP) GetCore() *coreruntime.Core {
	return a.core
}
