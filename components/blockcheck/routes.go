//go:build !minimal

package blockcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/gin-gonic/gin"
)

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
		if !readRequest(c, &request, feature.MaxGroupChangeBytes) {
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
		ports.HTTP.JSONObj(c, provider.options(c.Request.Context()), nil)
	})
	group.GET("/history", func(c *gin.Context) {
		if !ports.Auth.RequireScope(c, "blockcheck.history.read", "admin") {
			return
		}
		c.Header("Cache-Control", "no-store")
		view, groupID := c.Query("view"), c.Query("groupId")
		if view != "" && view != "all" {
			ports.HTTP.JSONObj(c, nil, feature.ErrInvalidHistory)
			return
		}
		history, err := readHistoryPage(c.Request.Context(), dbsqlite.DB(), time.Now(), historyFilter{
			all: view == "all", groupID: groupID, before: c.Query("before"),
		})
		ports.HTTP.JSONObj(c, history, err)
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
		catalog, err := readRunCatalog(ctx, dbsqlite.DB(), request.CatalogRevision, request.GroupID, false)
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
		catalog, err := readRunCatalog(ctx, dbsqlite.DB(), request.CatalogRevision, request.GroupID, request.All)
		if err != nil {
			ports.HTTP.JSONObj(c, nil, err)
			return
		}
		controller := http.NewResponseController(c.Writer)
		encoder := json.NewEncoder(c.Writer)
		capture := feature.NewHistoryCapture(catalog, request)
		db := dbsqlite.DB()
		var historyID string
		var historyPosition int
		var historyErr error
		err = component.RunSuite(ctx, ports.Host, catalog, request, provider, permitTarget, func(event feature.SuiteEvent) error {
			if event.Kind == "started" {
				c.Header("Content-Type", "application/x-ndjson")
				c.Header("X-Accel-Buffering", "no")
				historyID = event.Job.OperationID
				writeCtx, stop := context.WithTimeout(ctx, time.Second)
				historyErr = beginHistory(writeCtx, db, historyID, *event.StartedAt)
				stop()
			}
			run, item := capture.Observe(event)
			if item != nil && historyErr == nil {
				writeCtx, stop := context.WithTimeout(ctx, time.Second)
				historyErr = writeHistoryGroup(db.WithContext(writeCtx), historyID, historyPosition, *item)
				stop()
				historyPosition++
			}
			if run != nil {
				writeCtx, stop := context.WithTimeout(ctx, time.Second)
				saved := historyErr == nil && retainHistory(writeCtx, db, *run) == nil
				stop()
				event.HistorySaved = &saved
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
