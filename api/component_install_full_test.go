//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func TestComponentEnableDisableUpdatesEnabledState(t *testing.T) {
	settingService := initSessionTestDB(t)
	if err := (&service.UserService{}).UpdateFirstUser("admin", "component-password"); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(t.TempDir(), "components", "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{
		Version: 1,
		Profile: "full",
		Binary:  "full",
		Components: []installstate.InstalledComponent{
			{ID: "panel-update-ui", Delivery: manifest.DeliveryInProcess, Installed: true},
			{ID: "telegram", Delivery: manifest.DeliveryInProcess, Installed: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	reconciles := 0
	service.RegisterComponentSettingsReconciler(func(context.Context) error {
		reconciles++
		return nil
	})
	t.Cleanup(func() { service.RegisterComponentSettingsReconciler(nil) })

	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		NewAPIHandler(router.Group("/api"), nil)
	})
	token, csrfCookies := issueSecurityCSRFToken(t, router, cookies)

	disable := performCSRFRequest(router, http.MethodPost, "/api/update/components/telegram/disable", token, csrfCookies...)
	assertComponentActionSuccess(t, disable, true)
	assertTelegramInstallState(t, true, false)

	token, csrfCookies = issueSecurityCSRFToken(t, router, csrfCookies)
	enable := performCSRFRequest(router, http.MethodPost, "/api/update/components/telegram/enable", token, csrfCookies...)
	assertComponentActionSuccess(t, enable, true)
	assertTelegramInstallState(t, true, true)
	if reconciles < 2 {
		t.Fatalf("reconciles = %d, want enable/disable operations to reconcile runtime state", reconciles)
	}
}

func TestComponentInstallRemoveAreInstallerManaged(t *testing.T) {
	settingService := initSessionTestDB(t)
	if err := (&service.UserService{}).UpdateFirstUser("admin", "component-password"); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(t.TempDir(), "components", "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{
		Version: 1,
		Profile: "full",
		Binary:  "full",
		Components: []installstate.InstalledComponent{
			{ID: "panel-update-ui", Delivery: manifest.DeliveryInProcess, Installed: true},
			{ID: "telegram", Delivery: manifest.DeliveryInProcess, Installed: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		NewAPIHandler(router.Group("/api"), nil)
	})
	token, csrfCookies := issueSecurityCSRFToken(t, router, cookies)

	remove := performComponentRemoveRequest(router, "/api/update/components/telegram/remove", token, "component-password", csrfCookies...)
	assertComponentActionSuccess(t, remove, false)
	assertTelegramInstallState(t, true, true)

	token, csrfCookies = issueSecurityCSRFToken(t, router, csrfCookies)
	install := performCSRFRequest(router, http.MethodPost, "/api/update/components/telegram/install", token, csrfCookies...)
	assertComponentActionSuccess(t, install, false)
	assertTelegramInstallState(t, true, true)
}

func assertComponentActionSuccess(t *testing.T, recorder *httptest.ResponseRecorder, want bool) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
	}
	if response.Success != want {
		t.Fatalf("success = %v, want %v body=%s", response.Success, want, recorder.Body.String())
	}
}

func performComponentRemoveRequest(router *gin.Engine, path string, token string, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	body := url.Values{"password": {password}}.Encode()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set(csrfHeader, token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	router.ServeHTTP(recorder, req)
	_ = service.StopAuditWriter(context.Background())
	return recorder
}

func assertTelegramInstallState(t *testing.T, installed bool, enabled bool) {
	t.Helper()
	ids, err := installstate.InstalledIDs(availableComponentManifestsForTest())
	if err != nil {
		t.Fatal(err)
	}
	_, exists := ids["telegram"]
	if exists != installed {
		t.Fatalf("telegram installed = %v, want %v", exists, installed)
	}
	gotEnabled, err := enabledstate.Enabled(manifest.Manifest{
		ID:             "telegram",
		Name:           "Telegram",
		Version:        "1",
		Delivery:       manifest.DeliveryInProcess,
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotEnabled != enabled {
		t.Fatalf("telegram enabled = %v, want %v", gotEnabled, enabled)
	}
}

func availableComponentManifestsForTest() []manifest.Manifest {
	components := registry.Components()
	manifests := make([]manifest.Manifest, 0, len(components))
	for _, component := range components {
		manifests = append(manifests, component.Manifest)
	}
	return manifests
}
