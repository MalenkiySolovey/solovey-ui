//go:build !minimal

package remoteoutboundsubscriptions

import (
	"context"
	_ "embed"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	remotesubhttp "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/api"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	remotesettings "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/settings"
	remotesubservice "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/service"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	outboundentities "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

var runtimeHooks = struct {
	sync.Mutex
	unregisterClientOutbounds func()
	unregisterOutboundSave    func()
	unregisterOutboundDelete  func()
	unregisterMetadata        func()
	unregisterBackup          func()
	unregisterOptionKeys      func()
	unregisterSettings        func()
}{}

func init() {
	registry.Register(registry.Component{
		Manifest:  componentManifest,
		Lifecycle: component{},
	})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[remotesubhttp.Deps]{
		Build:    remoteOutboundDeps,
		Register: remotesubhttp.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct{}

func (component) Migrate(context.Context, lifecycle.Context) error {
	return remotesub.EnsureSchema(dbsqlite.DB())
}

func (component) DropData(context.Context, lifecycle.Context) error {
	if err := remotesub.DropSchema(dbsqlite.DB()); err != nil {
		return err
	}
	return deleteComponentSettings(remotesettings.AllKeys())
}

func (component) Start(_ context.Context, ctx lifecycle.Context) error {
	registerRuntimeHooks()
	remotesubservice.StartRemoteOutboundAutoRefresh(ctx.Host.API.Runtime)
	return nil
}

func (component) Stop(ctx context.Context) error {
	unregisterRuntimeHooks()
	return remotesubservice.StopRemoteOutboundAutoRefresh(ctx)
}

func registerRuntimeHooks() {
	runtimeHooks.Lock()
	defer runtimeHooks.Unlock()
	if runtimeHooks.unregisterClientOutbounds == nil {
		runtimeHooks.unregisterClientOutbounds = localsub.RegisterClientOutboundContributor(id, appendClientRemoteOutbounds)
	}
	if runtimeHooks.unregisterSettings == nil {
		runtimeHooks.unregisterSettings = registerSettingContribution()
	}
	if runtimeHooks.unregisterOutboundSave == nil {
		runtimeHooks.unregisterOutboundSave = service.RegisterOutboundSaveHook(id, reconcileOutboundLinks)
	}
	if runtimeHooks.unregisterOutboundDelete == nil {
		runtimeHooks.unregisterOutboundDelete = outboundentities.RegisterDeleteHook(id, remotesub.UnsyncConnectionsForDeletedOutbound)
	}
	if runtimeHooks.unregisterMetadata == nil {
		runtimeHooks.unregisterMetadata = outboundentities.RegisterMetadataAnnotator(id, remotesub.AnnotateManagedOutboundMetadata)
	}
	if runtimeHooks.unregisterBackup == nil {
		runtimeHooks.unregisterBackup = dbbackup.RegisterTables(id, []dbbackup.TableContribution{
			{Name: "remote_outbound_subscriptions", Model: &remotesub.RemoteOutboundSubscription{}},
			{Name: "remote_outbound_groups", Model: &remotesub.RemoteOutboundGroup{}},
			{Name: "remote_outbound_connections", Model: &remotesub.RemoteOutboundConnection{}},
			{Name: "remote_outbound_group_connections", Model: &remotesub.RemoteOutboundGroupConnection{}},
		})
	}
	if runtimeHooks.unregisterOptionKeys == nil {
		runtimeHooks.unregisterOptionKeys = model.RegisterOutboundOptionStripKeys(
			id,
			"componentBadges",
			"componentDeleteHintKey",
			"componentNotice",
			"remoteOutboundManaged",
			"remoteOutboundConnection",
			"remoteOutboundSubscription",
			"remoteOutboundGroups",
		)
	}
}

func unregisterRuntimeHooks() {
	runtimeHooks.Lock()
	defer runtimeHooks.Unlock()
	if runtimeHooks.unregisterMetadata != nil {
		runtimeHooks.unregisterMetadata()
		runtimeHooks.unregisterMetadata = nil
	}
	if runtimeHooks.unregisterOutboundSave != nil {
		runtimeHooks.unregisterOutboundSave()
		runtimeHooks.unregisterOutboundSave = nil
	}
	if runtimeHooks.unregisterOutboundDelete != nil {
		runtimeHooks.unregisterOutboundDelete()
		runtimeHooks.unregisterOutboundDelete = nil
	}
	if runtimeHooks.unregisterClientOutbounds != nil {
		runtimeHooks.unregisterClientOutbounds()
		runtimeHooks.unregisterClientOutbounds = nil
	}
	if runtimeHooks.unregisterSettings != nil {
		runtimeHooks.unregisterSettings()
		runtimeHooks.unregisterSettings = nil
	}
	if runtimeHooks.unregisterBackup != nil {
		runtimeHooks.unregisterBackup()
		runtimeHooks.unregisterBackup = nil
	}
	if runtimeHooks.unregisterOptionKeys != nil {
		runtimeHooks.unregisterOptionKeys()
		runtimeHooks.unregisterOptionKeys = nil
	}
}

func appendClientRemoteOutbounds(ctx localsub.ClientOutboundContributionContext, set *localsub.OutboundSet) error {
	remoteOutbounds, remoteTags, err := remotesub.OutboundsForClientLinksWithOptions(ctx.DB, ctx.RawLinks, remoteClientConversionOptions(ctx.Target))
	if err != nil {
		return err
	}
	set.AppendMany(remoteOutbounds, remoteTags)
	return nil
}

func remoteClientConversionOptions(target string) subconversion.ClientConversionOptions {
	settings := remotesettings.Reader{}
	groupAdaptation := ""
	rawPolicy := ""
	if value, err := settings.GetRemoteGroupAdaptation(); err == nil {
		groupAdaptation = value
	}
	if value, err := settings.GetRemoteConversionPolicy(); err == nil {
		rawPolicy = value
	}
	return subconversion.ClientConversionOptions{
		Target: target,
		Policy: subconversion.ParsePolicy(rawPolicy, groupAdaptation),
	}
}

func reconcileOutboundLinks(tx *gorm.DB) error {
	_, err := remotesub.ReconcileOutboundLinks(tx)
	return err
}

func remoteOutboundDeps(host componenthost.APIDeps) remotesubhttp.Deps {
	return remotesubhttp.Deps{
		Service:        &remotesubservice.RemoteOutboundService{Runtime: host.Runtime},
		RequireScope:   host.Auth.RequireScope,
		Actor:          host.Request.Actor,
		ValidateTarget: host.Request.ValidateTarget,
		JSONObj:        host.HTTP.JSONObj,
		JSONMsg:        host.HTTP.JSONMsg,
	}
}

func deleteComponentSettings(keys []string) error {
	if len(keys) == 0 || dbsqlite.DB() == nil {
		return nil
	}
	return dbsqlite.DB().Where("key IN ?", keys).Delete(&model.Setting{}).Error
}
