//go:build !minimal

package panelupdateui

import (
	_ "embed"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	panelupdatehttp "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/api"
	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

func init() {
	registry.Register(registry.Component{
		Manifest:  componentManifest,
		Lifecycle: lifecycle.Noop{},
	})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[panelupdatehttp.Deps]{
		Build:    panelUpdateDeps,
		Register: panelupdatehttp.RegisterRoutes,
	}.RegisterRoutes)
}

func panelUpdateDeps(host componenthost.APIDeps) panelupdatehttp.Deps {
	componentManager := panelupdateservice.NewManager(panelupdateservice.NewRuntimeManager(&service.ConfigService{}))
	return panelupdatehttp.Deps{
		Settings:         &service.SettingService{},
		Versions:         &service.VersionService{},
		Manager:          panelupdateservice.NewUpdateManager(),
		Components:       componentManager,
		ComponentManager: componentManager,
		LoginUser:        host.Auth.LoginUser,
		RemoteIP:         host.Request.RemoteIP,
		Hostname:         host.Request.Hostname,
		RequireScope:     host.Auth.RequireScope,
		RequireStepUp:    host.Auth.RequireStepUp,
		CheckPassword:    host.Auth.CheckPassword,
		CheckRateLimit:   host.Rate.CheckLoginRateLimit,
		RecordFailure:    host.Rate.RecordLoginFailure,
		ResetFailures:    host.Rate.ResetLoginFailures,
		UserKey:          host.Rate.LoginRateLimitUserKey,
		AllowCheck:       host.Update.AllowForcedUpdateCheck,
		Audit:            host.Audit.Audit,
		JSONObj:          host.HTTP.JSONObj,
		JSONMsg:          host.HTTP.JSONMsg,
	}
}
