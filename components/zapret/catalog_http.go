//go:build !minimal

package zapret

import (
	"errors"
	"io"
	"net/http"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func registerRoutes(routes *gin.RouterGroup, ports componenthost.Ports) {
	group := routes.Group("/components/zapret")
	group.GET("/strategies", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.read", "read", "admin") {
			return
		}
		view, err := service.NewConfigServiceWithRuntime(nil).ZapretCatalog()
		ports.HTTP.JSONObj(c, view, err)
	})
	group.POST("/strategies", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.write", "admin") {
			return
		}
		var change service.ZapretCatalogChange
		if err := readRequest(c, &change); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		view, err := service.NewConfigServiceWithRuntime(nil).SaveZapretCatalog(change, ports.Request.Actor(c))
		ports.HTTP.JSONObj(c, view, err)
	})
	group.POST("/strategies/preview", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.preview", "admin") {
			return
		}
		var draft strategy.Draft
		if err := readRequest(c, &draft); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		draft.ID = "ui-zapret-preview"
		value, err := strategy.SealDraft(draft)
		ports.HTTP.JSONObj(c, value, err)
	})
	group.POST("/diagnose", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.diagnose", "admin") {
			return
		}
		if ports.Rate.CheckRateLimit == nil || ports.Request.RemoteIP == nil {
			ports.HTTP.JSONObj(c, nil, errors.New("diagnostic rate limiter is unavailable"))
			return
		}
		if _, err := ports.Rate.CheckRateLimit("zapret:" + ports.Request.RemoteIP(c)); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		var request service.ZapretDiagnosticRequest
		if err := readRequest(c, &request); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		result, err := service.NewConfigServiceWithRuntime(nil).DiagnoseZapret(c.Request.Context(), request)
		ports.HTTP.JSONObj(c, result, err)
	})
}

func readRequest(c *gin.Context, target any) error {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	if err != nil || broker.DecodeRawPayload(data, target) != nil {
		return errors.New("invalid Zapret request")
	}
	return nil
}
