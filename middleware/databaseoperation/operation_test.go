package databaseoperation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

func operationDatabase(t *testing.T) string {
	t.Helper()
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operations.db")
	if err := dbsqlite.Init(path); err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := dbsqlite.DB().Create(&model.Setting{Key: "operation-marker", Value: "original"}).Error; err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForAdmissionFreeze(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, release, err := dbsqlite.AcquireOperation(ctx)
		if errors.Is(err, dbsqlite.ErrMaintenance) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		release()
		runtime.Gosched()
	}
}

func TestHTTPAdmissionSpansQueryGapAndRejectsLateRequests(t *testing.T) {
	operationDatabase(t)
	original := dbsqlite.DB()
	entered, proceed, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(proceed) }) }
	t.Cleanup(resume)
	router := gin.New()
	router.Use(Middleware())
	router.GET("/work", func(c *gin.Context) {
		var marker model.Setting
		if err := dbsqlite.DB().WithContext(c.Request.Context()).Where("key = ?", "operation-marker").First(&marker).Error; err != nil {
			c.Status(500)
			return
		}
		close(entered)
		<-proceed // no SQL resource is held across this semantic gap
		if marker.Value != "original" || dbsqlite.DB() != original {
			c.Status(500)
			return
		}
		if err := dbsqlite.DB().WithContext(c.Request.Context()).Model(&model.Setting{}).Where("key = ?", "operation-marker").Update("value", "finished").Error; err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	router.GET("/status", func(c *gin.Context) { c.Status(200) })
	response := httptest.NewRecorder()
	go func() { router.ServeHTTP(response, httptest.NewRequest("GET", "/work", nil)); close(finished) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	maintenanceDone := make(chan *dbsqlite.Maintenance, 1)
	maintenanceError := make(chan error, 1)
	go func() {
		owner, err := dbsqlite.BeginMaintenance(ctx)
		if err != nil {
			maintenanceError <- err
			return
		}
		maintenanceDone <- owner
	}()
	waitForAdmissionFreeze(t)
	select {
	case owner := <-maintenanceDone:
		owner.End()
		t.Fatal("maintenance crossed the HTTP query gap")
	case err := <-maintenanceError:
		t.Fatal(err)
	default:
	}
	late := httptest.NewRecorder()
	router.ServeHTTP(late, httptest.NewRequest("GET", "/status", nil))
	if late.Code != http.StatusServiceUnavailable || late.Header().Get("Retry-After") != "1" {
		t.Fatalf("late request status=%d", late.Code)
	}
	resume()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if response.Code != 204 {
		t.Fatalf("admitted request status=%d", response.Code)
	}
	var owner *dbsqlite.Maintenance
	select {
	case owner = <-maintenanceDone:
	case err := <-maintenanceError:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer owner.End()
	var marker model.Setting
	if err := original.Where("key = ?", "operation-marker").First(&marker).Error; err != nil || marker.Value != "finished" {
		t.Fatalf("admitted operation result: %v", err)
	}
	owner.End()
	available := httptest.NewRecorder()
	router.ServeHTTP(available, httptest.NewRequest("GET", "/status", nil))
	if available.Code != 200 {
		t.Fatal("maintenance completion did not reopen admission")
	}
}

func TestRestoreRequestUpgradeAndResponseUsePublishedGeneration(t *testing.T) {
	path := operationDatabase(t)
	original := dbsqlite.DB()
	router := gin.New()
	router.Use(Middleware(), Middleware()) // real nested web/API adapters reuse the lease
	router.POST("/restore", func(c *gin.Context) {
		owner, err := dbsqlite.BeginMaintenance(c.Request.Context())
		if err != nil {
			c.Status(500)
			return
		}
		defer owner.End()
		private := owner.Context(c.Request.Context())
		if err := dbsqlite.CloseForFileSwap(private); err != nil {
			c.Status(500)
			return
		}
		if dbsqlite.DB() == nil {
			c.Status(500)
			return
		}
		if err := dbsqlite.InitContext(private, path); err != nil {
			c.Status(500)
			return
		}
		if dbsqlite.DB() == original {
			c.Status(500)
			return
		}
		if err := dbsqlite.DB().WithContext(private).Model(&model.Setting{}).Where("key = ?", "operation-marker").Update("value", "candidate").Error; err != nil {
			c.Status(500)
			return
		}
		owner.End()
		var marker model.Setting
		if err := dbsqlite.DB().WithContext(c.Request.Context()).Where("key = ?", "operation-marker").First(&marker).Error; err != nil {
			c.Status(500)
			return
		}
		c.String(200, marker.Value)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/restore", nil))
	if response.Code != 200 || response.Body.String() != "candidate" {
		t.Fatalf("restore response=%d %s", response.Code, response.Body.String())
	}
	owner, err := dbsqlite.BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	owner.End()
}
