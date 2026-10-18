package api

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestRuntimeFixtureDrainsInjectedWritersBeforeDatabaseHandoff(t *testing.T) {
	initSessionTestDB(t)
	previous := service.DefaultRuntime()
	runtime := service.NewRuntime(nil)
	t.Cleanup(func() { closeAPITestRuntime(t, runtime) })
	token := model.Tokens{UserId: 1, Desc: "injected runtime fixture", TokenHash: "fixture-hash", Scope: "read"}
	if err := dbsqlite.DB().Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&service.UserService{Runtime: runtime}).RecordTokenUse(token.Id, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if err := (&service.AuditService{Runtime: runtime}).Record(service.AuditEvent{
		Actor: "fixture", Event: "injected_runtime_before_handoff", Resource: "database",
	}); err != nil {
		t.Fatal(err)
	}
	closeAPITestRuntime(t, runtime)
	if service.DefaultRuntime() != previous {
		t.Fatal("fixture teardown replaced the default runtime")
	}
	if err := dbsqlite.DB().First(&token, token.Id).Error; err != nil || token.LastUsedAt == 0 || token.LastUsedIP != "192.0.2.1" {
		t.Fatalf("injected token-use writer did not drain before database retirement: err=%v", err)
	}
	var count int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "injected_runtime_before_handoff").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("injected writer did not drain before database retirement: count=%d err=%v", count, err)
	}
	initAPITestDB(t, filepath.Join(t.TempDir(), "second.db"))
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "injected_runtime_before_handoff").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("injected writer crossed database handoff: count=%d err=%v", count, err)
	}
}
