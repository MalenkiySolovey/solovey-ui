//go:build linux

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	deploymentbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/service"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Run by tests/installer/deployment-persistence-runtime.sh with a real systemd
// service and socket activation. All observations come from the production host
// adapter; no synthetic broker payload or attestor replaces the runtime path.
func TestDeploymentSystemdPersistenceBroker(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_DEPLOYMENT_BROKER") != "1" {
		t.Skip("requires real systemd runner")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	account, err := user.Lookup("solovey-ui")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{{
		Name: "panel", UID: uint32(uid), GID: uint32(gid), Executable: executable, ExecutableDigest: broker.Digest(data),
		Device: uint64(stat.Dev), Inode: stat.Ino, CgroupUnit: "solovey-ui.service", CgroupPolicy: broker.CgroupRequired,
		CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1, Roles: []broker.Role{broker.RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := broker.NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := broker.OpenFileJournal(broker.DefaultJournalRoot, strings.TrimSpace(string(boot)))
	if err != nil {
		t.Fatal(err)
	}
	registry := broker.NewRegistry()
	if err := deploymentbroker.RegisterHandlers(registry, deploymentbroker.BackendSystemdNative, journal); err != nil {
		t.Fatal(err)
	}
	server, err := broker.NewServer(registry, journal, attestor, strings.TrimSpace(string(boot)))
	if err != nil {
		t.Fatal(err)
	}
	server.Audit = func(event broker.AuditEvent) {
		fmt.Printf("DEPLOYMENT_AUDIT verb=%s phase=%s result=%s\n", event.Verb, event.Phase, event.ResultClass)
	}
	listeners, err := broker.OpenTransport(broker.SystemdActivated)
	if err != nil {
		t.Fatal(err)
	}
	defer listeners.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := server.Serve(ctx, listeners.Listeners[broker.RolePanel], broker.RolePanel); err != nil {
		t.Fatal(err)
	}
}

func TestDeploymentSystemdPersistenceClient(t *testing.T) {
	if os.Getenv("SOLOVEY_TEST_DEPLOYMENT_CLIENT") != "1" {
		t.Skip("requires real systemd runner")
	}
	if os.Geteuid() == 0 {
		t.Fatal("panel must be nonroot")
	}
	processStatus, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CapInh:", "CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"} {
		if !strings.Contains(string(processStatus), field+"\t0000000000000000") {
			t.Fatalf("panel capability field %s is not zero", field)
		}
	}
	if !strings.Contains(string(processStatus), "NoNewPrivs:\t1") {
		t.Fatal("panel NoNewPrivileges missing")
	}
	if os.Getenv("SUI_DB_FOLDER") != "/var/lib/solovey-ui/db" {
		t.Fatal("installed database injection missing")
	}
	initAPITestDB(t, filepath.Join(os.Getenv("SUI_DB_FOLDER"), "solovey-ui.db"))
	t.Cleanup(func() { closeAPITestDB(t) })
	db := dbsqlite.DB()
	provider := deploymentservice.RuntimeProvider()
	if _, ok := provider.(*deploymentservice.SystemdBrokerProvider); !ok {
		t.Fatalf("installed provider injection selected %T", provider)
	}
	manager := deploymentservice.NewManager(deploymentservice.Repository{DB: dbsqlite.DB}, provider)
	router, cookies := newAuthenticatedTestRouter(t, &service.SettingService{}, func(router *gin.Engine) {
		apiService := NewApiService()
		apiService.Deployment = manager
		(&APIHandler{ApiService: apiService}).registerDeploymentRoutes(router.Group("/api"))
	})
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// These are the first broker-backed requests. No capability/Doctor probe or
	// test dial may activate the broker before the ordinary authenticated Status.
	for call := 0; call < 2; call++ {
		response := deploymentRequest(router, cookies, http.MethodGet, "/api/v1/operations/deployment/status", "")
		var result struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || !result.Success {
			t.Fatalf("cold Status call %d failed: http=%d", call+1, response.Code)
		}
	}
	t.Log("FIRST_STATUS_AND_SECOND_STATUS=PASS")
	commits := 0
	// A second real WAL connection commits at every posture write boundary.
	if err := db.Callback().Create().Before("gorm:create").Register("deployment_wal_writer", func(tx *gorm.DB) {
		if tx.Statement.Table != "deployment_state_v1" {
			return
		}
		_, err := pool.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "deployment-runtime-writer", fmt.Sprint(commits))
		if err != nil {
			_ = tx.AddError(err)
			return
		}
		commits++
	}); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 6; iteration++ {
		for _, endpoint := range []string{"status", "doctor"} {
			response := deploymentRequest(router, cookies, http.MethodGet, "/api/v1/operations/deployment/"+endpoint, "")
			var result struct {
				Success bool            `json:"success"`
				Msg     string          `json:"msg"`
				Obj     json.RawMessage `json:"obj"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || !result.Success {
				t.Fatalf("public %s failed: http=%d reason=%s", endpoint, response.Code, result.Msg)
			}
			if endpoint == "doctor" {
				var report domain.DoctorReport
				if err := json.Unmarshal(result.Obj, &report); err != nil {
					t.Fatal(err)
				}
				if report.Posture == nil || report.Posture.Runtime != domain.RuntimeSystemdNative || report.Posture.Profile != domain.NativeHardened {
					t.Fatal("real native posture missing")
				}
				state, err := manager.Repository.State(context.Background())
				trusted := report.Healthy && report.Posture.Validate(time.Now()) == nil
				if err != nil || state.Trusted != trusted || state.DoctorRevision != report.Revision || state.PostureRevision != report.Posture.Revision {
					t.Fatalf("state authority mismatch: %v", err)
				}
				if iteration == 0 {
					t.Logf("NATIVE_PROJECTION state=%s healthy=%t trusted=%t unit_file=%s", report.State, report.Healthy, state.Trusted, report.Posture.Systemd.UnitFileState)
				}
				var snapshot model.DeploymentDoctorSnapshot
				if err := db.Where("revision = ?", state.DoctorRevision).Take(&snapshot).Error; err != nil {
					t.Fatal(err)
				}
				var persisted domain.DoctorReport
				if err := json.Unmarshal(snapshot.PayloadJSON, &persisted); err != nil || persisted.Revision != report.Revision {
					t.Fatal("snapshot authority mismatch")
				}
			}
		}
	}
	if commits != 12 {
		t.Fatalf("concurrent commits=%d", commits)
	}
	var integrity string
	if err := db.Raw("PRAGMA integrity_check").Scan(&integrity).Error; err != nil || integrity != "ok" {
		t.Fatal("database integrity failed")
	}
	t.Log("REAL_SYSTEMD_DOCTOR_SQLITE_PUBLIC=PASS; STATUS_DOCTOR_CYCLES=6; CONCURRENT_WRITES=12; SNAPSHOT_REVISION_COHERENCE=PASS")
}
