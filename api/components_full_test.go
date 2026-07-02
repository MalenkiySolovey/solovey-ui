//go:build !minimal

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func init() {
	registerAPIComponentFixture("import-xui", "Import XUI", []string{"database"}, registerImportXUIFixtureRoutes)
	registerAPIComponentFixture("observability-extra", "Observability Extra", []string{"observability"}, registerObservabilityFixtureRoutes)
	registerAPIComponentFixture("paid-subscriptions", "Paid Subscriptions", nil, registerPaidFixtureRoutes)
	registerAPIComponentFixture("panel-update-ui", "Panel Update UI", []string{"update"}, registerUpdateFixtureRoutes)
	registerAPIComponentFixture("remote-outbound-subscriptions", "Remote Outbound Subscriptions", nil, registerRemoteOutboundFixtureRoutes)
	registerAPIComponentFixture("telegram", "Telegram", []string{"telegram"}, registerTelegramFixtureRoutes)
}

func registerAPIComponentFixture(id string, name string, scopes []string, register registry.APIRouteRegistrar) {
	if _, exists := registry.ComponentByID(id); exists {
		return
	}
	registry.Register(registry.Component{
		Manifest: manifest.Manifest{
			ID:             id,
			Name:           name,
			Version:        "1",
			Delivery:       manifest.DeliveryInProcess,
			DefaultEnabled: true,
			TokenScopes:    scopes,
		},
		Lifecycle: lifecycle.Noop{},
	})
	registry.RegisterAPIRoutes(id, register)
}

func registerImportXUIFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	for _, spec := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/import-xui"},
		{http.MethodPost, "/import-xui/plan"},
		{http.MethodPost, "/import-xui/apply"},
		{http.MethodPost, "/import-xui/rollback"},
		{http.MethodGet, "/import-xui/reports"},
	} {
		route := spec
		g.Handle(route.method, route.path, func(c *gin.Context) {
			if !deps.Auth.RequireScope(c, "database", "admin", "database") {
				return
			}
			c.JSON(http.StatusOK, Msg{Success: true})
		})
	}
	return nil
}

func registerObservabilityFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	serviceUnderTest := &ApiService{Runtime: deps.Runtime}
	telemetry := serviceUnderTest.coreTelemetryHandler()
	g.GET("/security/audit", telemetry.GetSecurityAudit)
	g.GET("/observability/history", func(c *gin.Context) {
		if !deps.Auth.RequireScope(c, "observability", "observability", "admin") {
			return
		}
		c.JSON(http.StatusOK, Msg{Success: true})
	})
	g.GET("/observability/core-history", func(c *gin.Context) {
		if !deps.Auth.RequireScope(c, "observability", "observability", "admin") {
			return
		}
		c.JSON(http.StatusOK, Msg{Success: true})
	})
	return nil
}

func registerPaidFixtureRoutes(g *gin.RouterGroup, _ componenthost.APIDeps) error {
	for _, spec := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/paidsub/bindings"},
		{http.MethodPost, "/paidsub/bindings"},
		{http.MethodGet, "/paidsub/tariffs"},
		{http.MethodPost, "/paidsub/tariffs"},
		{http.MethodGet, "/paidsub/orders"},
		{http.MethodGet, "/paidsub/status"},
		{http.MethodPost, "/paidsub/refund"},
		{http.MethodPost, "/paidsub/broadcast"},
	} {
		route := spec
		g.Handle(route.method, route.path, func(c *gin.Context) {
			c.JSON(http.StatusOK, Msg{Success: true})
		})
	}
	return nil
}

func registerRemoteOutboundFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	for _, spec := range []struct {
		method  string
		path    string
		allowed []string
	}{
		{http.MethodGet, "/remote-outbound-subscriptions", []string{"admin", "read", "write"}},
		{http.MethodGet, "/remote-outbound-subscriptions/collected", []string{"admin", "read", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/save", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/delete", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/refresh", []string{"admin", "write"}},
		{http.MethodGet, "/remote-outbound-subscriptions/test", []string{"admin", "write"}},
		{http.MethodGet, "/remote-outbound-subscriptions/test-all", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/groups/save", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/groups/bulk", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/groups/delete", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/groups/connections", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/groups/outbounds", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/connections/group", []string{"admin", "write"}},
		{http.MethodPost, "/remote-outbound-subscriptions/connections/sync", []string{"admin", "write"}},
		{http.MethodGet, "/remote-outbound-subscriptions/connections/test", []string{"admin", "write"}},
	} {
		route := spec
		g.Handle(route.method, route.path, func(c *gin.Context) {
			if !deps.Auth.RequireScope(c, "remoteOutboundSubscriptions", route.allowed...) {
				return
			}
			c.JSON(http.StatusOK, Msg{Success: true})
		})
	}
	return nil
}

func registerTelegramFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	group := g.Group("/telegram")
	group.POST("/test", func(c *gin.Context) {
		if !deps.Auth.RequireScope(c, "telegram", "admin") {
			return
		}
		deps.Audit.Audit(c, deps.Request.Actor(c), "telegram_test", "telegram", service.AuditSeverityWarn, map[string]any{
			"errorClass": "disabled",
		})
		c.JSON(http.StatusOK, Msg{Success: true, Obj: gin.H{"success": false, "errorClass": "disabled"}})
	})
	for _, path := range []string{"/backup", "/backup/run"} {
		routePath := path
		group.POST(routePath, func(c *gin.Context) {
			if !deps.Auth.RequireScope(c, "telegram", "telegram", "admin") {
				return
			}
			key := deps.Request.Actor(c)
			if key == "" {
				key = deps.Request.RemoteIP(c)
			}
			retryAfter, err := deps.Rate.CheckRateLimit(key)
			if err != nil {
				c.Header("Retry-After", "1")
				if retryAfter > 0 {
					c.Header("Retry-After", strconv.Itoa(int((retryAfter+time.Second-1)/time.Second)))
				}
				deps.Audit.Audit(c, key, "tg_backup_failed", "database", service.AuditSeverityWarn, map[string]any{
					"channel":    "telegram",
					"errorClass": "rate_limited",
				})
				c.JSON(http.StatusTooManyRequests, Msg{
					Success: false,
					Msg:     "telegramBackup: rate_limited",
					Obj:     gin.H{"errorClass": "rate_limited", "trigger": "manual"},
				})
				return
			}
			deps.Audit.Audit(c, deps.Request.Actor(c), "tg_backup_failed", "database", service.AuditSeverityWarn, map[string]any{
				"channel":    "telegram",
				"errorClass": "disabled",
			})
			c.JSON(http.StatusServiceUnavailable, Msg{
				Success: false,
				Msg:     "telegramBackup: disabled",
				Obj:     gin.H{"errorClass": "disabled", "trigger": "manual"},
			})
		})
	}
	return nil
}

func registerUpdateFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	group := g.Group("/update")
	group.GET("/status", func(c *gin.Context) { c.JSON(http.StatusOK, Msg{Success: true}) })
	group.GET("/components", func(c *gin.Context) { c.JSON(http.StatusOK, Msg{Success: true}) })
	group.POST("/check", func(c *gin.Context) { c.JSON(http.StatusOK, Msg{Success: true}) })
	group.POST("/apply", func(c *gin.Context) { c.JSON(http.StatusOK, Msg{Success: true}) })
	for _, action := range []string{"enable", "disable", "install", "remove"} {
		action := action
		group.POST("/components/:id/"+action, func(c *gin.Context) {
			if !deps.Auth.RequireScope(c, "update", "admin", "update") {
				return
			}
			c.JSON(http.StatusOK, Msg{Success: true})
		})
	}
	return nil
}
