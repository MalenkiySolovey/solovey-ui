//go:build !minimal

package blockcheck

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBlockCheckDurableCatalogRevision(t *testing.T) {
	manifest, registered := durableowner.Lookup(feature.ID)
	if !registered || !slices.Equal(manifest.Database.Settings, []string{feature.CatalogSettingKey}) || manifest.DefaultEnabled {
		t.Fatalf("incorrect ComponentHost registration: %+v", manifest)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "blockcheck.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	current, err := readCatalog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	request := feature.CatalogChange{Action: "delete", ID: "youtube", ExpectedRevision: current.Revision()}
	next, err := changeCatalog(ctx, db, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changeCatalog(ctx, db, request); !errors.Is(err, feature.ErrStaleCatalog) {
		t.Fatalf("stale transaction was not rejected: %v", err)
	}
	observed, err := readCatalog(ctx, db)
	if err != nil || observed.Revision() != next.Revision() || len(observed.Groups) != len(current.Groups)-1 {
		t.Fatalf("durable read did not match commit: %v", err)
	}
	if _, exists := observed.Group("youtube"); exists {
		t.Fatal("removed default was silently recreated")
	}
}

func TestBlockCheckAuthorizationAndBoundsPrecedeExecution(t *testing.T) {
	for _, spec := range []struct {
		method, path, scope string
		limit               int
	}{
		{"GET", "/groups", "blockcheck.groups.read", 0}, {"POST", "/groups", "blockcheck.groups.write", feature.MaxCatalogBytes + 2048},
		{"GET", "/options", "blockcheck.options.read", 0}, {"POST", "/run", "blockcheck.run", 8 << 10},
		{"POST", "/run/stream", "blockcheck.run", feature.MaxSuiteRequestBytes}, {"POST", "/run/cancel", "blockcheck.run", 512},
	} {
		t.Run(spec.method+spec.path, func(t *testing.T) {
			g := gin.New()
			allow := false
			registerRoutes(g.Group("/api"), componenthost.Ports{Auth: componenthost.AuthDeps{RequireScope: func(c *gin.Context, scope string, roles ...string) bool {
				if scope != spec.scope || !slices.Equal(roles, []string{"admin"}) {
					t.Fatalf("unbound authorization: %q %v", scope, roles)
				}
				if !allow {
					c.AbortWithStatus(http.StatusForbidden)
				}
				return allow
			}}, HTTP: componenthost.HTTPDeps{JSONObj: func(c *gin.Context, _ any, err error) {
				if err == nil {
					t.Fatal("invalid request could reach execution")
				}
				c.JSON(http.StatusBadRequest, gin.H{"msg": err.Error()})
			}}}, nil, localProvider{})
			req := func(body string) int {
				w := httptest.NewRecorder()
				g.ServeHTTP(w, httptest.NewRequest(spec.method, "/api/components/blockcheck"+spec.path, strings.NewReader(body)))
				return w.Code
			}
			if status := req(`{"unexpected":true}`); status != http.StatusForbidden {
				t.Fatalf("unauthorized request status %d", status)
			}
			if spec.method == http.MethodPost {
				allow = true
				for _, body := range []string{`{"unexpected":true}`, strings.Repeat(" ", spec.limit) + `{}`, `{} {}`} {
					if status := req(body); status != http.StatusBadRequest {
						t.Fatalf("invalid request status %d", status)
					}
				}
			}
		})
	}
}
