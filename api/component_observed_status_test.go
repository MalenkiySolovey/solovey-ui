//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/gin-gonic/gin"
)

func TestComponentInventoryPreservesEligibilityAndSerializesLifecycle(t *testing.T) {
	initSessionTestDB(t)
	prepareComponentRouteMetadata(t)
	metadata, _, err := installstate.Load(installstate.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	const absentID = "test-inventory-binary-absent"
	metadata.Components = append(metadata.Components, installstate.InstalledComponent{
		ID: absentID, Delivery: manifest.DeliveryInProcess, Installed: true,
	})
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, metadata); err != nil {
		t.Fatal(err)
	}
	publisher := state.NewLifecyclePublisher()
	publisher.RecordStart("telegram", false)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	(&APIHandler{}).registerComponentStatusRoutes(router.Group("/api"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/components", nil))
	var response struct {
		Success bool                      `json:"success"`
		Obj     componentRuntimeInventory `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !response.Success {
		t.Fatalf("inventory failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var foundTelegram, foundAbsent bool
	for _, component := range response.Obj.Components {
		switch component.ID {
		case "telegram":
			foundTelegram = true
			if !component.Installed || !component.Enabled || !component.Active || !component.AvailableInBinary || component.ObservedLifecycle.Phase != state.LifecycleStartFailed || component.ObservedLifecycle.ObservedAt == nil {
				t.Fatalf("legacy eligibility or observed failure lost on wire: %+v", component)
			}
		case absentID:
			foundAbsent = true
			if !component.Installed || component.Active || component.AvailableInBinary || component.Version != "" || component.ObservedLifecycle.Phase != state.LifecycleBinaryAbsent {
				t.Fatalf("absent owner misrepresented on wire: %+v", component)
			}
		}
	}
	if !foundTelegram || !foundAbsent {
		t.Fatalf("inventory omitted owners: %+v", response.Obj.Components)
	}
}
