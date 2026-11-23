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
		writeCatalog(c, ports, service.NewConfigServiceWithRuntime(nil))
	})
	group.POST("/strategies", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.write", "admin") {
			return
		}
		var change catalogChange
		if err := readRequest(c, &change); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		if err := change.prepare(); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		config := service.NewConfigServiceWithRuntime(nil)
		err := config.SaveZapretCatalog(change.Action, change.ID, change.ExpectedRevision, ports.Request.Actor(c), change.mutate)
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		writeCatalog(c, ports, config)
	})
	group.POST("/strategies/preview", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.preview", "admin") {
			return
		}
		var request struct {
			strategy.Draft
			ExpectedRevision string `json:"expectedRevision,omitempty"`
		}
		if err := readRequest(c, &request); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		request.Draft.ID = "ui-zapret-preview"
		var catalog strategy.Catalog
		if request.ExpectedRevision != "" || len(request.ListRefs) > 0 {
			var err error
			catalog, err = readCatalogAtRevision(request.ExpectedRevision)
			if err != nil {
				ports.HTTP.JSONObj(c, nil, err)
				return
			}
		}
		value, err := catalog.SealDraft(request.Draft)
		ports.HTTP.JSONObj(c, value, err)
	})
	group.GET("/strategies/:id/export", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.read", "read", "admin") {
			return
		}
		catalog, err := readCatalogAtRevision(c.Query("expectedRevision"))
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		transfer, err := catalog.Export(c.Param("id"))
		ports.HTTP.JSONObj(c, transfer, err)
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
		var request diagnosticRequest
		if err := readRequest(c, &request); err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		result, err := service.NewConfigServiceWithRuntime(nil).DiagnoseZapret(c.Request.Context(), request.OutboundRef, request.authorize)
		ports.HTTP.JSONObj(c, result, err)
	})
}

func readCatalogAtRevision(revision string) (strategy.Catalog, error) {
	if len(revision) != 64 {
		return strategy.Catalog{}, errors.New("missing Zapret catalog revision")
	}
	snapshot, err := service.NewConfigServiceWithRuntime(nil).ZapretCatalog()
	if err != nil {
		return strategy.Catalog{}, err
	}
	catalog, err := readCatalog(snapshot.Catalog)
	if err == nil && catalog.Revision() != revision {
		err = errors.New("the Zapret strategy catalog changed; reload before continuing")
	}
	return catalog, err
}

func writeCatalog(c *gin.Context, ports componenthost.Ports, config *service.ConfigService) {
	snapshot, err := config.ZapretCatalog()
	if err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	view, err := makeCatalogView(snapshot)
	ports.HTTP.JSONObj(c, view, err)
}

func readRequest(c *gin.Context, target any) error {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	if err != nil || broker.DecodeRawPayload(data, target) != nil {
		return errors.New("invalid Zapret request")
	}
	return nil
}
