package update

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-gonic/gin"
)

type fakeSettings struct{ channel string }

func (s *fakeSettings) GetUpdateChannel() string { return s.channel }
func (s *fakeSettings) SetUpdateChannel(channel string) error {
	s.channel = channel
	return nil
}

type fakeVersions struct{ target serviceupdate.ReleaseTarget }

func (v fakeVersions) CheckForChannel(channel string, _ bool) serviceupdate.VersionInfo {
	return serviceupdate.VersionInfo{Channel: channel, Latest: v.target.Tag, UpdateAvailable: true, AssetAvailable: true}
}
func (v fakeVersions) ResolveTarget(string) (serviceupdate.ReleaseTarget, error) {
	return v.target, nil
}

type fakeManager struct {
	applied bool
	target  serviceupdate.ReleaseTarget
}

func (m *fakeManager) Status() serviceupdate.UpdateJob {
	return serviceupdate.UpdateJob{Stage: serviceupdate.UpdateStageIdle}
}
func (m *fakeManager) Apply(target serviceupdate.ReleaseTarget, _ string) error {
	m.applied, m.target = true, target
	return nil
}

func testDeps(manager *fakeManager, passwordOK bool, audit func(map[string]any)) Deps {
	settings := &fakeSettings{channel: "main"}
	target := serviceupdate.ReleaseTarget{Channel: "main", Tag: "v2.0.0", Version: "2.0.0"}
	return Deps{
		Settings: settings, Versions: fakeVersions{target: target}, Manager: manager,
		LoginUser:      func(*gin.Context) string { return "admin" },
		RemoteIP:       func(*gin.Context) string { return "127.0.0.1" },
		CheckPassword:  func(_, _, _ string) bool { return passwordOK },
		CheckRateLimit: func(string) error { return nil },
		RecordFailure:  func(string) {}, ResetFailures: func(string) {},
		UserKey:    func(user string) string { return "user|" + user },
		AllowCheck: func() bool { return true },
		Audit: func(_ *gin.Context, _, _, _, _ string, details map[string]any) {
			if audit != nil {
				audit(details)
			}
		},
		JSONObj: func(context *gin.Context, object any, err error) {
			context.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": object})
		},
		JSONMsg: func(context *gin.Context, _ string, err error) {
			context.JSON(http.StatusOK, gin.H{"success": err == nil})
		},
	}
}

func performUpdateRequest(t *testing.T, deps Deps, body url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), deps)
	request := httptest.NewRequest(http.MethodPost, "/api/update/apply", strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestApplyRequiresPasswordBeforeResolvingOrApplying(t *testing.T) {
	manager := &fakeManager{}
	response := performUpdateRequest(t, testDeps(manager, true, nil), url.Values{
		"channel": {"main"}, "targetVersion": {"v2.0.0"},
	})
	if response.Code != http.StatusOK || manager.applied {
		t.Fatalf("status=%d applied=%v body=%s", response.Code, manager.applied, response.Body.String())
	}
}

func TestApplyReauthAndConfirmedVersionStartUpdateWithoutAuditingPassword(t *testing.T) {
	manager := &fakeManager{}
	var audited map[string]any
	response := performUpdateRequest(t, testDeps(manager, true, func(details map[string]any) { audited = details }), url.Values{
		"channel": {"main"}, "targetVersion": {"v2.0.0"}, "password": {"top-secret"},
	})
	if response.Code != http.StatusOK || !manager.applied || manager.target.Version != "2.0.0" {
		t.Fatalf("status=%d applied=%v target=%#v", response.Code, manager.applied, manager.target)
	}
	for key, value := range audited {
		if strings.Contains(strings.ToLower(key), "password") || strings.Contains(valueAsString(value), "top-secret") {
			t.Fatalf("password leaked into audit: %#v", audited)
		}
	}
}

func valueAsString(value any) string {
	text, _ := value.(string)
	return text
}

func TestTargetVersionMustMatchCheckedRelease(t *testing.T) {
	target := serviceupdate.ReleaseTarget{Tag: "v2.0.0", Version: "2.0.0"}
	if !targetVersionMatches("2.0.0", target) || targetVersionMatches("2.0.1", target) || targetVersionMatches("", target) {
		t.Fatal("target confirmation contract failed")
	}
}
