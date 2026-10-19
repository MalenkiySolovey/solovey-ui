package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/gin-gonic/gin"
)

func TestCoreJournalRequiresAuthenticationAndBoundsOwnerMetadata(t *testing.T) {
	settings := initSessionTestDB(t)
	db := dbsqlite.DB()
	if err := db.Create(&model.UpdateJournal{OperationID: "update-operation:one", Revision: 1, State: "DOWNLOADING", Event: "preparing", CreatedAt: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UpdateJournal{OperationID: "update-operation:one", Revision: 2, State: "DOWNLOADED", Event: strings.Repeat("x", 4096), ReasonCode: "Bearer deterministic fake secret", CreatedAt: 3}).Error; err != nil {
		t.Fatal(err)
	}
	operation := model.DataLifecycleOperation{OperationID: "data-operation:one", IdempotencyKey: "journal-fixture", Kind: "RESTORE", OwnerID: "core", State: "APPLIED", Revision: 3}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.DataLifecycleJournal{OperationID: operation.OperationID, Revision: 3, State: "APPLIED", Event: "restore_applied", CreatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	a := &APIHandler{}
	router, cookies := newAuthenticatedTestRouter(t, settings, func(r *gin.Engine) { r.GET("/journal", a.operationJournal) })
	unauthenticated := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/journal", nil))
	if unauthenticated.Code != http.StatusForbidden || strings.Contains(unauthenticated.Body.String(), "restore_applied") {
		t.Fatalf("unauthenticated journal=%d %s", unauthenticated.Code, unauthenticated.Body.String())
	}
	cursor := operationcoordination.JournalCursor{}
	seen := map[string]bool{}
	for range 3 {
		path := fmt.Sprintf("/journal?limit=1&updateAfter=%d&dataAfter=%d", cursor.Update, cursor.Data)
		response := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, path, nil), cookies...)
		var msg struct {
			Success bool                              `json:"success"`
			Object  operationcoordination.JournalPage `json:"obj"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &msg); err != nil || !msg.Success || len(msg.Object.Items) != 1 {
			t.Fatalf("journal=%s err=%v", response.Body.String(), err)
		}
		item := msg.Object.Items[0]
		if seen[item.JournalRef] || item.CorrelationID == "" || item.JobID == "" || strings.Contains(response.Body.String(), "fake secret") || len(response.Body.Bytes()) > 4096 {
			t.Fatalf("unsafe/duplicate index: %s", response.Body.String())
		}
		seen[item.JournalRef] = true
		cursor = msg.Object.Next
	}
	if cursor.Update != 2 || cursor.Data != 1 {
		t.Fatalf("journal lost owner lane: %+v", cursor)
	}
	for _, path := range []string{"/journal?limit=201", "/journal?limit=0", "/journal?updateAfter=-1", "/journal?dataAfter=invalid", "/journal?updateAfter=18446744073709551615"} {
		if response := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, path, nil), cookies...); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid query accepted: %s %d", path, response.Code)
		}
	}
}
