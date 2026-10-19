package app

import (
	"context"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/database/schema"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

type updatePanelProof struct{ at time.Time }

func (updatePanelProof) ResourceID() string { return "core:panel:web" }
func (c updatePanelProof) Check(context.Context) componenthealth.Result {
	return componenthealth.WithEvidence(componenthealth.Result{ResourceID: c.ResourceID(), Status: componenthealth.StatusOK,
		Check: "panel_listener", FactCode: "listener_ready"}, "core", componenthealth.RuntimeReady, "panel_listener",
		componenthealth.Revision(configidentity.GetVersion()), c.at, 30*time.Second)
}

func TestUpdateAcceptanceUsesCurrentSchemaAndFreshPanelProof(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	application := NewApp()
	if err := application.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Stop)
	stop, err := componenthealth.Register(updatePanelProof{time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	operation := model.UpdateOperation{OperationID: "update-operation:proof", Version: configidentity.GetVersion(), RestartClass: "panel"}
	result := application.updateHealth(t.Context(), operation)
	if !result.Ready || len(result.Evidence) != 1 || result.Evidence[0].Evidence.ProofKind != componenthealth.RuntimeReady {
		t.Fatalf("current accepted schema %s failed: %+v", schema.CurrentCoreVersion, result)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "coreSchemaVersion").Update("value", "1.11").Error; err != nil {
		t.Fatal(err)
	}
	if result := application.updateHealth(t.Context(), operation); result.Ready {
		t.Fatal("obsolete schema accepted")
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "coreSchemaVersion").Update("value", schema.CurrentCoreVersion).Error; err != nil {
		t.Fatal(err)
	}
	stop()
	stopStale, err := componenthealth.Register(updatePanelProof{time.Now().Add(-10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	defer stopStale()
	if result := application.updateHealth(t.Context(), operation); result.Ready {
		t.Fatal("stale panel status accepted as fresh release evidence")
	}
}
