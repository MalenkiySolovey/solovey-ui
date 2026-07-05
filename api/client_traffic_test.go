package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestAPIV2ResetTrafficRequiresWriteScopeAndPreservesClient(t *testing.T) {
	initSessionTestDB(t)
	client := model.Client{
		Enable:    true,
		Name:      "alice",
		Inbounds:  []byte("[]"),
		Links:     []byte("[]"),
		Config:    []byte("{}"),
		Up:        12,
		Down:      34,
		TotalUp:   100,
		TotalDown: 200,
	}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	readToken, err := (&service.UserService{}).AddToken("admin", 0, "read", "read")
	if err != nil {
		t.Fatal(err)
	}
	writeToken, err := (&service.UserService{}).AddToken("admin", 0, "write", "write")
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAPIv2Handler(router.Group("/apiv2"))

	readRecorder := performResetTrafficRequest(router, readToken, client.Id)
	if readRecorder.Code != http.StatusForbidden {
		t.Fatalf("read token should be forbidden, got %d", readRecorder.Code)
	}

	var stored model.Client
	if err := dbsqlite.DB().Where("id = ?", client.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Up != 12 || stored.Down != 34 {
		t.Fatalf("read token changed counters: %#v", stored)
	}

	writeRecorder := performResetTrafficRequest(router, writeToken, client.Id)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("write token should be allowed, got %d: %s", writeRecorder.Code, writeRecorder.Body.String())
	}
	var msg Msg
	if err := json.Unmarshal(writeRecorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("reset request failed: %s", msg.Msg)
	}
	if err := dbsqlite.DB().Where("id = ?", client.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Up != 0 || stored.Down != 0 || stored.TotalUp != 112 || stored.TotalDown != 234 {
		t.Fatalf("traffic counters were not reset into totals: %#v", stored)
	}

	flushAPIAudit(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "client_traffic_reset").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "admin" {
		t.Fatalf("unexpected audit actor: %s", event.Actor)
	}
}

func performResetTrafficRequest(router *gin.Engine, token string, clientID uint) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/apiv2/resetTraffic?id="+strconv.FormatUint(uint64(clientID), 10), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, req)
	return recorder
}
