package deployment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func TestPackageManagedDoctorPersistsWithoutNativeAuthority(t *testing.T) {
	proof := openWrtOwnerProof(t)
	now := time.Now().UTC()
	provider := &OpenWrtPackageManagedProvider{now: func() time.Time { return now },
		loadOwner:   func() (deploymentidentity.ApplicationOwnerContractProcdV1, error) { return proof, nil },
		brokerProbe: func(context.Context) (string, bool) { return strings.Repeat("b", 64), true }}
	verifyProviderPersistence(t, provider, domain.PackageManagedOpenWrt)
}

func TestDockerRuntimeDoctorPersistence(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_DOCKER_PERSISTENCE") != "1" {
		t.Skip("requires real Docker runner")
	}
	if !DetectedDocker() {
		t.Fatal("real container evidence absent")
	}
	verifyProviderPersistence(t, NewDockerProvider(), domain.DockerHost)
}

func verifyProviderPersistence(t *testing.T, provider Provider, profile domain.ProfileID) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	if err := dbsqlite.Init(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	manager := NewManager(Repository{DB: dbsqlite.DB}, provider)
	ctx := context.Background()
	for iteration := 0; iteration < 3; iteration++ {
		posture, err := manager.Status(ctx)
		if err != nil || posture.Profile != profile || posture.Systemd != nil {
			t.Fatalf("portable status: %v", err)
		}
		report, err := manager.Doctor(ctx)
		if err != nil || report.Posture == nil || report.Posture.Profile != profile {
			t.Fatalf("portable doctor: %v", err)
		}
		if report.Capabilities.Migrate != domain.Unavailable || report.Capabilities.Rollback != domain.Unavailable {
			t.Fatal("native mutation authority leaked")
		}
		state, err := manager.Repository.State(ctx)
		if err != nil || state.DoctorRevision != report.Revision {
			t.Fatalf("portable revision: %v", err)
		}
		var snapshot model.DeploymentDoctorSnapshot
		if err := dbsqlite.DB().Where("revision = ?", state.DoctorRevision).Take(&snapshot).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("PROVIDER_DOCTOR_SQLITE=PASS profile=%s", profile)
}
