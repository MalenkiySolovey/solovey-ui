//go:build !minimal

package fallbackhtml

import (
	"context"
	_ "embed"
	"errors"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	fallbackapi "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/api"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

var runtimeHooks = struct {
	sync.Mutex
	unregisterBackup func()
	restoreHookName  string
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

func (component) Migrate(context.Context, lifecycle.Context) error {
	return fallbackdomain.EnsureSchema(dbsqlite.DB())
}

func (component) DropData(context.Context, lifecycle.Context) error {
	fallbackservice.DefaultRuntime.Stop()
	db := dbsqlite.DB()
	var joined error
	if db != nil && db.Migrator().HasTable(&model.InboundDraft{}) {
		joined = errors.Join(joined, db.Where("source LIKE ?", "fallback-html:%").Delete(&model.InboundDraft{}).Error)
	}
	joined = errors.Join(joined, fallbackdomain.DropSchema(db))
	joined = errors.Join(joined, fallbackservice.RemoveStorage())
	return joined
}

func (component) Start(_ context.Context, _ lifecycle.Context) error {
	registerRuntimeHooks()
	return fallbackservice.DefaultRuntime.Start(dbsqlite.DB())
}

func (component) Stop(context.Context) error {
	unregisterRuntimeHooks()
	fallbackservice.DefaultRuntime.Stop()
	return nil
}

func registerRuntimeHooks() {
	runtimeHooks.Lock()
	defer runtimeHooks.Unlock()
	if runtimeHooks.unregisterBackup == nil {
		runtimeHooks.unregisterBackup = dbbackup.RegisterTables(id, []dbbackup.TableContribution{
			{Name: "fallback_html_sites", Model: &fallbackdomain.Site{}},
			{Name: "fallback_html_pages", Model: &fallbackdomain.Page{}},
			{Name: "fallback_html_redirects", Model: &fallbackdomain.Redirect{}},
			{Name: "fallback_html_assets", Model: &fallbackdomain.Asset{}},
			{Name: "fallback_html_publishes", Model: &fallbackdomain.Publish{}},
			{Name: "fallback_html_node_publications", Model: &fallbackdomain.NodePublication{}},
			{Name: "fallback_html_node_endpoints", Model: &fallbackdomain.NodeEndpoint{}},
			{Name: "fallback_html_publish_files", Model: &fallbackdomain.PublishFile{}},
			{Name: "fallback_html_publish_redirects", Model: &fallbackdomain.PublishRedirect{}},
			{Name: "fallback_html_safety_reports", Model: &fallbackdomain.SafetyReport{}},
			{Name: "fallback_html_template_sources", Model: &fallbackdomain.TemplateSource{}},
			{Name: "fallback_html_self_steal_drafts", Model: &fallbackdomain.SelfStealDraft{}},
			{Name: "fallback_html_runtime_targets", Model: &fallbackdomain.RuntimeTarget{}},
			{Name: "fallback_html_external_resources", Model: &fallbackdomain.ExternalResource{}},
			{Name: "fallback_html_events", Model: &fallbackdomain.Event{}},
		})
	}
	if runtimeHooks.restoreHookName == "" {
		runtimeHooks.restoreHookName = id + ".restore"
		dbhooks.RegisterImportPostOpenHook(runtimeHooks.restoreHookName, func(ctx context.Context) error {
			return fallbackservice.HandleRestorePostOpen(ctx, dbsqlite.DB())
		})
	}
}

func unregisterRuntimeHooks() {
	runtimeHooks.Lock()
	defer runtimeHooks.Unlock()
	if runtimeHooks.unregisterBackup != nil {
		runtimeHooks.unregisterBackup()
		runtimeHooks.unregisterBackup = nil
	}
	if runtimeHooks.restoreHookName != "" {
		dbhooks.RegisterImportPostOpenHook(runtimeHooks.restoreHookName, nil)
		runtimeHooks.restoreHookName = ""
	}
}

func fallbackDeps(host componenthost.APIDeps) fallbackapi.Deps {
	return fallbackapi.Deps{
		Service:      fallbackservice.New(dbsqlite.DB(), fallbackservice.DefaultRuntime),
		RequireScope: host.Auth.RequireScope,
		Actor:        host.Request.Actor,
		JSONObj:      host.HTTP.JSONObj,
		JSONMsg:      host.HTTP.JSONMsg,
	}
}
