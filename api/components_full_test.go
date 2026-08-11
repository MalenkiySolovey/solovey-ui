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
	registerAPIComponentFixture("fallback-html", "Fallback HTML", []string{"public-site"}, registerFallbackHTMLFixtureRoutes)
	registerAPIComponentFixture("import-xui", "Import XUI", []string{"database"}, registerImportXUIFixtureRoutes)
	registerAPIComponentFixture("observability-extra", "Observability Extra", []string{"observability"}, registerObservabilityFixtureRoutes)
	registerAPIComponentFixture("paid-subscriptions", "Paid Subscriptions", nil, registerPaidFixtureRoutes)
	registerAPIComponentFixture("panel-update-ui", "Panel Update UI", []string{"update"}, registerUpdateFixtureRoutes)
	registerAPIComponentFixture("remote-outbound-subscriptions", "Remote Outbound Subscriptions", nil, registerRemoteOutboundFixtureRoutes)
	registerAPIComponentFixture("server-protection", "Server Protection", []string{"server-protection:read", "server-protection:write", "server-protection:apply"}, registerServerProtectionFixtureRoutes)
	registerAPIComponentFixture("telegram", "Telegram", []string{"telegram"}, registerTelegramFixtureRoutes)
}

func registerServerProtectionFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	group := g.Group("/components/server-protection")
	for _, spec := range []struct {
		method  string
		path    string
		allowed []string
	}{
		{http.MethodGet, "/status", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodGet, "/settings", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPut, "/settings", []string{"admin", "server-protection:write"}},
		{http.MethodGet, "/resources", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodGet, "/profiles", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/profiles", []string{"admin", "server-protection:write"}},
		{http.MethodPut, "/profiles/:id", []string{"admin", "server-protection:write"}},
		{http.MethodDelete, "/profiles/:id", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/profiles/:id/reattach", []string{"admin", "server-protection:write"}},
		{http.MethodGet, "/events", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodDelete, "/events", []string{"admin", "server-protection:write"}},
		{http.MethodGet, "/graylist", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodDelete, "/graylist", []string{"admin", "server-protection:write"}},
		{http.MethodGet, "/allowlist/ports", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/allowlist/ports", []string{"admin", "server-protection:write"}},
		{http.MethodDelete, "/allowlist/ports/:id", []string{"admin", "server-protection:write"}},
		{http.MethodGet, "/allowlist/ips", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/allowlist/ips", []string{"admin", "server-protection:write"}},
		{http.MethodDelete, "/allowlist/ips/:id", []string{"admin", "server-protection:write"}},
		{http.MethodGet, "/diagnostics", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodGet, "/fronting/status", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/fronting/preview", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/fronting/sync", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/fronting/apply", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/fronting/rollback", []string{"admin", "server-protection:apply"}},
		{http.MethodGet, "/fronting/operations/:operationId", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/firewall/preview", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/firewall/prepare", []string{"admin", "server-protection:apply"}},
		{http.MethodGet, "/operations", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/operations/:operationId/force-unlock", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/operations/:operationId/forget-state", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/firewall/apply", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/firewall/rollback", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/ports/prepare", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/ports/apply", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/ports/rollback", []string{"admin", "server-protection:apply"}},
		{http.MethodGet, "/native-fallback/status", []string{"admin", "server-protection:read", "server-protection:write", "server-protection:apply"}},
		{http.MethodPost, "/native-fallback/preview", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/native-fallback/prepare", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/native-fallback/apply", []string{"admin", "server-protection:apply"}},
		{http.MethodPost, "/native-fallback/rollback", []string{"admin", "server-protection:apply"}},
		// The shared CSRF matrix exercises every mutation path with POST,
		// including production PUT/DELETE routes. These aliases exist only in
		// this test registry and keep that transport-level matrix exhaustive.
		{http.MethodPost, "/settings", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/profiles/:id", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/events", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/graylist", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/allowlist/ports/:id", []string{"admin", "server-protection:write"}},
		{http.MethodPost, "/allowlist/ips/:id", []string{"admin", "server-protection:write"}},
	} {
		route := spec
		group.Handle(route.method, route.path, func(c *gin.Context) {
			if !deps.Auth.RequireScope(c, "serverProtection", route.allowed...) {
				return
			}
			c.JSON(http.StatusOK, Msg{Success: true})
		})
	}
	return nil
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

func registerFallbackHTMLFixtureRoutes(g *gin.RouterGroup, deps componenthost.APIDeps) error {
	group := g.Group("/components/fallback-html")
	group.GET("/health", func(c *gin.Context) {
		if !deps.Auth.RequireScope(c, "publicSite", "admin", "read", "write", "public-site") {
			return
		}
		c.JSON(http.StatusOK, Msg{Success: true})
	})
	return nil
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
