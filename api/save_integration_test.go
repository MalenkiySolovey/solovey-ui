package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

func authedSaveRouter(t *testing.T) (*gin.Engine, string, []*http.Cookie) {
	t.Helper()
	settingService := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settingService)
	login := performCSRFRequest(router, http.MethodGet, "/login", "")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}
	token, cookies := issueSecurityCSRFToken(t, router, login.Result().Cookies())
	return router, token, cookies
}

func postSave(router *gin.Engine, token string, cookies []*http.Cookie, data string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("object", "clients")
	form.Set("action", "new")
	form.Set("data", data)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/save", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(csrfHeader, token)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func countClients(t *testing.T, name string) int64 {
	t.Helper()
	var count int64
	if err := dbsqlite.DB().Model(&model.Client{}).Where("name = ?", name).Count(&count).Error; err != nil {
		t.Fatalf("count clients: %v", err)
	}
	return count
}

func TestSaveClientCreatesExactlyOneRow(t *testing.T) {
	router, token, cookies := authedSaveRouter(t)
	recorder := postSave(router, token, cookies, `{"name":"single","enable":true,"inbounds":[],"links":[]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /api/save returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := countClients(t, "single"); got != 1 {
		t.Fatalf("one /api/save produced %d client rows (want 1)", got)
	}
}

func TestSaveDedupBlocksRapidDuplicateCreate(t *testing.T) {
	router, token, cookies := authedSaveRouter(t)
	payload := `{"name":"dupe","enable":true,"inbounds":[],"links":[]}`
	if recorder := postSave(router, token, cookies, payload); recorder.Code != http.StatusOK {
		t.Fatalf("first save returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := postSave(router, token, cookies, payload); recorder.Code != http.StatusOK {
		t.Fatalf("second save returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := countClients(t, "dupe"); got != 1 {
		t.Fatalf("rapid duplicate create produced %d rows (want 1)", got)
	}
}

func TestSaveDedupAllowsDistinctCreates(t *testing.T) {
	router, token, cookies := authedSaveRouter(t)
	if recorder := postSave(router, token, cookies, `{"name":"alpha","enable":true,"inbounds":[],"links":[]}`); recorder.Code != http.StatusOK {
		t.Fatalf("alpha save returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := postSave(router, token, cookies, `{"name":"beta","enable":true,"inbounds":[],"links":[]}`); recorder.Code != http.StatusOK {
		t.Fatalf("beta save returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	if alpha, beta := countClients(t, "alpha"), countClients(t, "beta"); alpha != 1 || beta != 1 {
		t.Fatalf("distinct creates: alpha=%d beta=%d (want 1,1)", alpha, beta)
	}
}
