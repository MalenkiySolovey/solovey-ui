//go:build !minimal

package fallbackhtml

import (
	"context"
	_ "embed"
	"errors"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackapi "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/api"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/gorm"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

var runtimeHooks = struct {
	sync.Mutex
	unregisterBackup    func()
	unregisterResources func()
	unregisterHealth    func()
	unregisterTargetsV2 func()
	restoreHookName     string
}{}

func init() {
	registry.Register(registry.Component{
		Manifest:  componentManifest,
		Lifecycle: component{},
	})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[fallbackapi.Deps]{
		Build:    fallbackDeps,
		Register: fallbackapi.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct{}

func (component) Migrate(ctx context.Context, _ lifecycle.Context) error {
	return (component{}).MigrateStaged(ctx, dbsqlite.DB())
}

func (component) MigrateStaged(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("fallback-html database is unavailable")
	}
	if err := fallbackdomain.EnsureSchema(db); err != nil {
		return err
	}
	if err := authority.EnsureSchema(db); err != nil {
		return err
	}
	return fallbackservice.ReconcileLegacySelfSteal(ctx, db, time.Now().UTC())
}

func (component) RehearseRestore(ctx context.Context, db *gorm.DB) error {
	return fallbackservice.HandleRestorePostOpen(ctx, db)
}

func (component) InspectDropAuthority(ctx context.Context, db *gorm.DB, now time.Time) lifecycle.DropAuthorityStatus {
	if db == nil {
		return lifecycle.DropAuthorityStatus{State: "UNAVAILABLE", ReasonCodes: []string{"owner_database_unavailable"}}
	}
	err := authority.WithMutationLockContext(ctx, func() error { return authority.GuardAllMutation(db.WithContext(ctx), now) })
	if err != nil {
		return lifecycle.DropAuthorityStatus{State: "BLOCKED", ReasonCodes: []string{"fallback_target_authority_present"}}
	}
	return lifecycle.DropAuthorityStatus{State: "VERIFIED_SAFE", ReasonCodes: []string{}}
}

func (component) DropData(context.Context, lifecycle.Context) error {
	return authority.WithMutationLock(func() error {
		db := dbsqlite.DB()
		if db != nil && db.Migrator().HasTable(&authority.ReservationModel{}) {
			if err := db.Transaction(func(tx *gorm.DB) error {
				return authority.GuardAllMutation(tx, time.Now().UTC())
			}); err != nil {
				return err
			}
		}
		fallbackservice.DefaultRuntime.Stop()
		var joined error
		if db != nil && db.Migrator().HasTable(&model.InboundDraft{}) {
			joined = errors.Join(joined, db.Where("source LIKE ?", "fallback-html:%").Delete(&model.InboundDraft{}).Error)
		}
		joined = errors.Join(joined, fallbackdomain.DropSchema(db))
		joined = errors.Join(joined, authority.DropSchema(db))
		joined = errors.Join(joined, fallbackservice.RemoveStorage())
		return joined
	})
}

func (component) Start(_ context.Context, _ lifecycle.Context) error {
	if err := registerRuntimeHooks(); err != nil {
		unregisterRuntimeHooks()
		return err
	}
	if err := fallbackservice.DefaultRuntime.Start(dbsqlite.DB()); err != nil {
		unregisterRuntimeHooks()
		return err
	}
	return nil
}

func (component) Stop(context.Context) error {
	unregisterRuntimeHooks()
	fallbackservice.DefaultRuntime.Stop()
	return nil
}

func registerRuntimeHooks() error {
	runtimeHooks.Lock()
	defer runtimeHooks.Unlock()
	if runtimeHooks.unregisterBackup == nil {
		runtimeHooks.unregisterBackup = dbbackup.RegisterTables(id, []dbbackup.TableContribution{
			{Name: "fallback_html_sites", Model: &fallbackdomain.Site{}},
			{Name: "fallback_html_pages", Model: &fallbackdomain.Page{}},
			{Name: "fallback_html_redirects", Model: &fallbackdomain.Redirect{}},
			{Name: "fallback_html_assets", Model: &fallbackdomain.Asset{}},
			{Name: "fallback_html_publishes", Model: &fallbackdomain.Publish{}},
			{Name: "fallback_html_publish_files", Model: &fallbackdomain.PublishFile{}},
			{Name: "fallback_html_publish_redirects", Model: &fallbackdomain.PublishRedirect{}},
			{Name: "fallback_html_safety_reports", Model: &fallbackdomain.SafetyReport{}},
			{Name: "fallback_html_template_sources", Model: &fallbackdomain.TemplateSource{}},
			{Name: "fallback_html_self_steal_drafts", Model: &fallbackdomain.SelfStealDraft{}},
			{Name: "fallback_html_runtime_targets", Model: &fallbackdomain.RuntimeTarget{}},
			{Name: "fallback_html_external_resources", Model: &fallbackdomain.ExternalResource{}},
			{Name: "fallback_html_events", Model: &fallbackdomain.Event{}},
			{Name: authority.ReservationTable, Model: &authority.ReservationModel{}},
			{Name: authority.ReservationReplayTable, Model: &authority.ReservationReplayModel{}},
		})
	}
	if runtimeHooks.unregisterResources == nil {
		unregister, err := hostresources.Register(publicSiteResourceContributor{db: dbsqlite.DB()})
		if err != nil {
			return err
		}
		runtimeHooks.unregisterResources = unregister
	}
	if runtimeHooks.unregisterHealth == nil {
		unregister, err := componenthealth.Register(fallbackHealthChecker{runtime: fallbackservice.DefaultRuntime})
		if err != nil {
			return err
		}
		runtimeHooks.unregisterHealth = unregister
	}
	if runtimeHooks.unregisterTargetsV2 == nil {
		unregister, err := neutralfallback.Default.RegisterV2(targetProvider{db: dbsqlite.DB(), runtime: fallbackservice.DefaultRuntime})
		if err != nil {
			return err
		}
		runtimeHooks.unregisterTargetsV2 = unregister
	}
	if runtimeHooks.restoreHookName == "" {
		runtimeHooks.restoreHookName = id + ".restore"
		dbhooks.RegisterImportPostOpenHook(runtimeHooks.restoreHookName, func(ctx context.Context) error {
			return fallbackservice.HandleRestorePostOpen(ctx, dbsqlite.DB())
		})
	}
	return nil
}

func unregisterRuntimeHooks() {
	runtimeHooks.Lock()
	defer runtimeHooks.Unlock()
	if runtimeHooks.unregisterBackup != nil {
		runtimeHooks.unregisterBackup()
		runtimeHooks.unregisterBackup = nil
	}
	if runtimeHooks.unregisterResources != nil {
		runtimeHooks.unregisterResources()
		runtimeHooks.unregisterResources = nil
	}
	if runtimeHooks.unregisterHealth != nil {
		runtimeHooks.unregisterHealth()
		runtimeHooks.unregisterHealth = nil
	}
	if runtimeHooks.unregisterTargetsV2 != nil {
		runtimeHooks.unregisterTargetsV2()
		runtimeHooks.unregisterTargetsV2 = nil
	}
	if runtimeHooks.restoreHookName != "" {
		dbhooks.RegisterImportPostOpenHook(runtimeHooks.restoreHookName, nil)
		runtimeHooks.restoreHookName = ""
	}
}

func fallbackDeps(host componenthost.APIDeps) fallbackapi.Deps {
	return fallbackapi.Deps{
		Service: fallbackservice.New(dbsqlite.DB(), fallbackservice.DefaultRuntime),
		ProviderStatus: func(ctx context.Context, siteID uint) (fallbackapi.ProviderStatusView, error) {
			return fallbackProviderStatus(ctx, dbsqlite.DB(), fallbackservice.DefaultRuntime, siteID, time.Now().UTC())
		},
		RequireScope: host.Auth.RequireScope,
		Actor:        host.Request.Actor,
		JSONObj:      host.HTTP.JSONObj,
		JSONMsg:      host.HTTP.JSONMsg,
	}
}
