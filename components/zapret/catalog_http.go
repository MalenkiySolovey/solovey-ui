//go:build !minimal

package zapret

import (
	"crypto/sha256"
	"errors"
	"fmt"
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
		ports.HTTP.JSONObj(c, struct {
			strategy.Record
			VisualEditable bool `json:"visualEditable"`
		}{value, err == nil && value.VisualEditable()}, err)
	})
	group.POST("/strategies/import/preview", func(c *gin.Context) { handlePreset(c, ports, true) })
	group.POST("/strategies/import", func(c *gin.Context) { handlePreset(c, ports, false) })
	group.GET("/strategies/lua-drafts", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.read", "admin") {
			return
		}
		writeLuaDrafts(c, ports)
	})
	group.POST("/strategies/lua-drafts", func(c *gin.Context) { handleLuaDraft(c, ports) })
	group.GET("/strategies/:id/export", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "zapret.strategies.read", "read", "admin") {
			return
		}
		catalog, err := readCatalogAtRevision(c.Query("expectedRevision"))
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		exportPreset(c, ports, catalog)
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

const maxPresetEnvelope = ((strategy.MaxPresetBytes+2)/3)*4 + 1024
const maxLuaDraftEnvelope = ((strategy.MaxCatalogBytes+2)/3)*4 + 2048

func writeLuaDrafts(c *gin.Context, ports componenthost.Ports) {
	c.Header("Cache-Control", "no-store")
	snapshot, err := service.NewConfigServiceWithRuntime(nil).ZapretCatalog()
	if err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	catalog, err := readCatalog(snapshot.Catalog)
	if err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	ports.HTTP.JSONObj(c, struct {
		Revision string                       `json:"revision"`
		Drafts   map[string]strategy.LuaDraft `json:"drafts"`
	}{catalog.Revision(), catalog.LuaDrafts}, nil)
}

func handleLuaDraft(c *gin.Context, ports componenthost.Ports) {
	if !ports.Auth.RequireScope(c, "zapret.strategies.write", "admin") {
		return
	}
	var request struct {
		Action           string `json:"action"`
		ID               string `json:"id"`
		ExpectedRevision string `json:"expectedRevision"`
		Draft            *struct {
			Origin    string  `json:"origin"`
			SourceRef string  `json:"sourceRef,omitempty"`
			Source    *[]byte `json:"source"`
		} `json:"draft,omitempty"`
	}
	if err := readRequestLimit(c, &request, maxLuaDraftEnvelope); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	switch request.Action {
	case "create", "edit", "delete":
	default:
		ports.HTTP.JSONObj(c, nil, errors.New("unknown Lua draft change"))
		return
	}
	change := catalogChange{Action: request.Action + "-lua-draft", ID: request.ID, ExpectedRevision: request.ExpectedRevision}
	if draft := request.Draft; draft != nil {
		if draft.Source == nil {
			ports.HTTP.JSONObj(c, nil, errors.New("missing Lua draft source"))
			return
		}
		change.luaDraft = &strategy.LuaDraft{Origin: draft.Origin, SourceRef: draft.SourceRef, Source: string(*draft.Source)}
	}
	if err := change.prepare(); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	config := service.NewConfigServiceWithRuntime(nil)
	if err := config.SaveZapretCatalog(change.Action, change.ID, change.ExpectedRevision, ports.Request.Actor(c), change.mutate); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	writeLuaDrafts(c, ports)
}

func handlePreset(c *gin.Context, ports componenthost.Ports, preview bool) {
	scope := "zapret.strategies.write"
	if preview {
		scope = "zapret.strategies.preview"
	}
	if !ports.Auth.RequireScope(c, scope, "admin") {
		return
	}
	var request struct {
		ExpectedRevision string               `json:"expectedRevision"`
		Preset           strategy.PresetInput `json:"preset"`
	}
	if err := readRequestLimit(c, &request, maxPresetEnvelope); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	change := catalogChange{Action: "import-preset", ExpectedRevision: request.ExpectedRevision, preset: &request.Preset}
	if err := change.prepare(); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	if preview {
		catalog, err := readCatalogAtRevision(change.ExpectedRevision)
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		_, review, err := catalog.PreparePreset(change.ID, request.Preset)
		ports.HTTP.JSONObj(c, review, err)
		return
	}
	config := service.NewConfigServiceWithRuntime(nil)
	if err := config.SaveZapretCatalog(change.Action, change.ID, change.ExpectedRevision, ports.Request.Actor(c), change.mutate); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return
	}
	writeCatalog(c, ports, config)
}

func exportPreset(c *gin.Context, ports componenthost.Ports, catalog strategy.Catalog) {
	id := c.Param("id")
	switch c.Query("format") {
	case "", "json":
		transfer, err := catalog.Export(id)
		ports.HTTP.JSONObj(c, transfer, err)
	case "zip":
		data, err := catalog.ExportPackage(id)
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="strategy.zip"`)
		c.Data(http.StatusOK, "application/zip", data)
	case "native":
		record, ok := catalog.Records[id]
		if !ok {
			ports.HTTP.JSONObj(c, nil, errors.New("native strategy does not exist"))
			return
		}
		if len(record.ListRefs) != 0 || len(record.Definition.BlobData) != 0 {
			ports.HTTP.JSONObj(c, nil, errors.New("export ZIP or JSON to keep this Strategy's managed lists and assets"))
			return
		}
		c.Header("Content-Disposition", `attachment; filename="strategy.txt"`)
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(record.Raw))
	default:
		ports.HTTP.JSONObj(c, nil, errors.New("unknown Zapret export format"))
	}
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
	if err == nil && ports.Request.Actor != nil {
		if actor := ports.Request.Actor(c); actor != "" {
			view.PreferenceKey = fmt.Sprintf("%x", sha256.Sum256([]byte("zapret-ui/preferences/v1:"+actor)))
		}
	}
	ports.HTTP.JSONObj(c, view, err)
}

func readRequest(c *gin.Context, target any) error {
	return readRequestLimit(c, target, 32<<10)
}

func readRequestLimit(c *gin.Context, target any, limit int64) error {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		return fmt.Errorf("the Zapret request exceeds its %d-byte transfer limit", limit)
	}
	if err != nil || broker.DecodeRawPayload(data, target) != nil {
		return errors.New("invalid Zapret request")
	}
	return nil
}
