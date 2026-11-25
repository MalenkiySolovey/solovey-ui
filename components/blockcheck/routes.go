//go:build !minimal

package blockcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errStorage = errors.New("blockcheck_catalog_storage_unavailable")

func registerRoutes(routes *gin.RouterGroup, ports componenthost.Ports, component *feature.Component, provider localProvider) {
	group := routes.Group("/components/blockcheck")
	group.GET("/groups", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.groups.read", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		catalog, err := readCatalog(c.Request.Context(), dbsqlite.DB())
		ports.HTTP.JSONObj(c, catalog.View(), err)
	})
	group.POST("/groups", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.groups.write", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		var request feature.CatalogChange
		if !readRequest(c, &request, feature.MaxCatalogBytes+2048) {
			ports.HTTP.JSONObj(c, nil, feature.ErrInvalidGroup)
			return
		}
		catalog, err := changeCatalog(c.Request.Context(), dbsqlite.DB(), request)
		if err == nil && ports.Audit.Audit != nil {
			ports.Audit.Audit(c, "blockcheck_groups_changed", feature.ID, "", "", map[string]any{"action": request.Action, "revision": catalog.Revision()})
		}
		ports.HTTP.JSONObj(c, catalog.View(), err)
	})
	group.GET("/options", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.options.read", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		ports.HTTP.JSONObj(c, provider.options(), nil)
	})
	group.POST("/run", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.run", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		var request feature.RunRequest
		if !readRequest(c, &request, 8<<10) {
			ports.HTTP.JSONObj(c, nil, feature.ErrInvalidRun)
			return
		}
		if !allowRun(c, ports) {
			return
		}
		ctx := c.Request.Context()
		catalog, err := readCatalog(ctx, dbsqlite.DB())
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		report, err := component.Run(ctx, ports.Host, catalog, request, provider, permitTarget)
		if err == nil && ports.Audit.Audit != nil {
			ports.Audit.Audit(c, "blockcheck_run", feature.ID, report.GroupID, "", map[string]any{
				"operation": report.Job.OperationID, "phase": report.Job.Phase, "mode": report.Path.Mode, "probes": len(report.Results),
			})
		}
		ports.HTTP.JSONObj(c, report, err)
	})
	group.POST("/run/stream", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.run", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		var request feature.SuiteRequest
		if !readRequest(c, &request, feature.MaxSuiteRequestBytes) {
			ports.HTTP.JSONObj(c, nil, feature.ErrInvalidRun)
			return
		}
		if !allowRun(c, ports) {
			return
		}
		ctx := c.Request.Context()
		catalog, err := readCatalog(ctx, dbsqlite.DB())
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		controller := http.NewResponseController(c.Writer)
		encoder := json.NewEncoder(c.Writer)
		err = component.RunSuite(ctx, ports.Host, catalog, request, provider, permitTarget, func(event feature.SuiteEvent) error {
			if event.Kind == "started" {
				c.Header("Content-Type", "application/x-ndjson")
				c.Header("X-Accel-Buffering", "no")
			}
			if event.Kind == "finished" && ports.Audit.Audit != nil {
				ports.Audit.Audit(c, "blockcheck_run", feature.ID, event.Job.ResourceID, "", map[string]any{
					"operation": event.Job.OperationID, "phase": event.Job.Phase, "mode": request.Path.Mode,
					"groups": event.Progress.GroupsDone, "probes": event.Progress.ProbesDone,
				})
			}
			// Same bounded write/idle behavior as the Core runtime-log stream.
			_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Second))
			defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
			if err := encoder.Encode(event); err != nil {
				return err
			}
			return controller.Flush()
		})
		if err != nil && !c.Writer.Written() && ctx.Err() == nil {
			ports.HTTP.JSONObj(c, nil, err)
		}
	})
	group.POST("/run/cancel", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.run", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		var request struct {
			OperationID string `json:"operationId"`
		}
		if !readRequest(c, &request, 512) {
			ports.HTTP.JSONObj(c, nil, feature.ErrInvalidRun)
			return
		}
		err := component.CancelSuite(ports.Host, request.OperationID)
		ports.HTTP.JSONObj(c, nil, err)
	})
}

func allowRun(c *gin.Context, ports componenthost.Ports) bool {
	if ports.Rate.CheckRateLimit == nil || ports.Request.RemoteIP == nil {
		ports.HTTP.JSONObj(c, nil, feature.ErrInactive)
		return false
	}
	if _, err := ports.Rate.CheckRateLimit("blockcheck:" + ports.Request.RemoteIP(c)); err != nil {
		ports.HTTP.JSONObj(c, nil, err)
		return false
	}
	return true
}

func readRequest(c *gin.Context, target any, limit int64) bool {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	if err != nil || !utf8.Valid(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(target) == nil && d.Decode(new(any)) == io.EOF
}

func readCatalog(ctx context.Context, db *gorm.DB) (feature.Catalog, error) {
	if db == nil {
		return feature.Catalog{}, errStorage
	}
	var row model.Setting
	err := db.WithContext(ctx).Where("key = ?", feature.CatalogSettingKey).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return feature.DecodeCatalog(nil)
	}
	if err != nil || row.Value == "" {
		return feature.Catalog{}, errStorage
	}
	return feature.DecodeCatalog([]byte(row.Value))
}

func changeCatalog(ctx context.Context, db *gorm.DB, request feature.CatalogChange) (catalog feature.Catalog, err error) {
	if db == nil {
		return catalog, errStorage
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := readCatalog(ctx, tx)
		if err != nil {
			return err
		}
		catalog, err = current.Apply(request)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(catalog)
		if err != nil {
			return feature.ErrInvalidCatalog
		}
		if err := store.UpsertValue(tx, feature.CatalogSettingKey, string(payload)); err != nil {
			return errStorage
		}
		return nil
	})
	return catalog, err
}
