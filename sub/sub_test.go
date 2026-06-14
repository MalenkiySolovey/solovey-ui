package sub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var subUUIDV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func initSubTestDB(t *testing.T) {
	t.Helper()
	resetSubDisplaySettingsCacheForTest()
	tempDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", tempDir)
	closeSubTestDB(database.GetDB())
	if err := database.InitDB(filepath.Join(tempDir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := database.GetDB()
	t.Cleanup(func() {
		closeSubTestDB(testDB)
	})
}

func closeSubTestDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
		time.Sleep(25 * time.Millisecond)
	}
}

func TestGetClientBySubIdPrefersSecretAndSupportsLegacyName(t *testing.T) {
	initSubTestDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:    true,
		Name:      "legacy-name",
		SubSecret: "secret-id",
		Inbounds:  []byte("[]"),
		Links:     []byte("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	subService := &SubService{}
	bySecret, err := subService.getClientBySubId("secret-id")
	if err != nil {
		t.Fatal(err)
	}
	if bySecret.Name != "legacy-name" {
		t.Fatalf("unexpected secret lookup client: %#v", bySecret)
	}

	byName, err := subService.getClientBySubId("legacy-name")
	if err != nil {
		t.Fatal(err)
	}
	if byName.SubSecret != "secret-id" {
		t.Fatalf("legacy lookup did not return expected client: %#v", byName)
	}
}

func TestGetClientBySubIdCanDisableLegacyName(t *testing.T) {
	initSubTestDB(t)
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(model.Setting{}).Where("key = ?", "subSecretRequired").Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:    true,
		Name:      "legacy-name",
		SubSecret: "secret-id",
		Inbounds:  []byte("[]"),
		Links:     []byte("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	subService := &SubService{}
	if _, err := subService.getClientBySubId("legacy-name"); err == nil {
		t.Fatal("legacy name lookup should be disabled when subSecretRequired=true")
	}
	if _, err := subService.getClientBySubId("secret-id"); err != nil {
		t.Fatalf("secret lookup should still work: %v", err)
	}
}

func TestEnsureClientSubSecretGeneratesUUIDV4(t *testing.T) {
	initSubTestDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "legacy-name",
		Inbounds: []byte("[]"),
		Links:    []byte("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	if err := (&SubService{}).ensureClientSubSecret(database.GetDB(), &client); err != nil {
		t.Fatal(err)
	}
	if !subUUIDV4Pattern.MatchString(client.SubSecret) {
		t.Fatalf("sub secret is not uuid-v4: %q", client.SubSecret)
	}

	var stored model.Client
	if err := database.GetDB().Where("id = ?", client.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SubSecret != client.SubSecret {
		t.Fatalf("sub secret was not persisted: %#v", stored)
	}
}

func TestSubSecretRequiredReturns404ForLegacyNameURL(t *testing.T) {
	initSubTestDB(t)
	resetRateLimitBucketsForTest()
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(model.Setting{}).Where("key = ?", "subSecretRequired").Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:    true,
		Name:      "legacy-name",
		SubSecret: "secret-id",
		Inbounds:  []byte("[]"),
		Links:     []byte("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewSubHandler(router.Group(""))

	legacyRecorder := httptest.NewRecorder()
	router.ServeHTTP(legacyRecorder, httptest.NewRequest(http.MethodGet, "/legacy-name", nil))
	if legacyRecorder.Code != http.StatusNotFound {
		t.Fatalf("legacy name URL should be hidden, got %d", legacyRecorder.Code)
	}

	secretRecorder := httptest.NewRecorder()
	router.ServeHTTP(secretRecorder, httptest.NewRequest(http.MethodGet, "/secret-id", nil))
	if secretRecorder.Code != http.StatusOK {
		t.Fatalf("secret URL should still work, got %d", secretRecorder.Code)
	}
}

func TestSafeSubscriptionHeadersRemovesControlCharacters(t *testing.T) {
	got := safeSubscriptionHeaders([]string{"ok\r\nInjected: bad"})[0]
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("header was not sanitized: %q", got)
	}
}

func TestSubscriptionHeadersUseConfiguredTitleAndURLs(t *testing.T) {
	initSubTestDB(t)
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"subTitle":      "Panel\r\nInjected: bad",
		"subSupportUrl": "https://example.com/support",
		"subProfileUrl": "https://example.com/profile",
		"subAnnounce":   "Maintenance\r\nInjected: bad",
	}
	for key, value := range settings {
		if err := database.GetDB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}

	cfg := cachedSubDisplaySettings(&service.SettingService{}, time.Now())
	headers := buildClientHeaders(&model.Client{Name: "alice"}, cfg)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/sub/alice", nil)
	(&SubHandler{}).addHeaders(c, headers)

	if title := recorder.Header().Get("Profile-Title"); strings.ContainsAny(title, "\r\n") || !strings.Contains(title, "Panel") {
		t.Fatalf("unexpected sanitized title: %q", title)
	}
	if recorder.Header().Get("Support-Url") != "https://example.com/support" {
		t.Fatalf("support URL header missing: %#v", recorder.Header())
	}
	if recorder.Header().Get("Profile-Web-Page-Url") != "https://example.com/profile" {
		t.Fatalf("profile URL header missing: %#v", recorder.Header())
	}
	if announce := recorder.Header().Get("Profile-Announcement"); strings.ContainsAny(announce, "\r\n") || !strings.Contains(announce, "Maintenance") {
		t.Fatalf("unexpected sanitized announce: %q", announce)
	}
}

func TestSubscriptionEnableSettingsDisableFormats(t *testing.T) {
	initSubTestDB(t)
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"subLinkEnable":  "false",
		"subJsonEnable":  "false",
		"subClashEnable": "false",
	} {
		if err := database.GetDB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}

	links := json.RawMessage(`[{"type":"external","uri":"https://example.com/sub"}]`)
	if got := (&LinkService{}).GetLinks(&links, "all", ""); len(got) != 0 {
		t.Fatalf("link subscriptions should be disabled, got %#v", got)
	}
	if _, _, err := (&JsonService{}).GetJson("missing", "json"); err == nil {
		t.Fatal("json subscription should be disabled before client lookup")
	}
	if _, _, err := (&ClashService{}).GetClash("missing"); err == nil {
		t.Fatal("clash subscription should be disabled before client lookup")
	}
}

func TestSubServerServesDefaultAndCustomFormatPaths(t *testing.T) {
	initSubTestDB(t)
	resetRateLimitBucketsForTest()
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"subJsonPath":  "/sing-json/",
		"subClashPath": "/sing-clash/",
	} {
		if err := database.GetDB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{}`),
		Inbounds:  json.RawMessage(`[]`),
		Links:     json.RawMessage(`[]`),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	server := NewServer()
	router, err := server.initRouter()
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/json/secret-id", "/sing-json/secret-id"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), `"outbounds"`) {
			t.Fatalf("%s did not return JSON subscription: %s", path, recorder.Body.String())
		}
	}

	for _, path := range []string{"/clash/secret-id", "/sing-clash/secret-id"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "proxy-groups:") {
			t.Fatalf("%s did not return Clash subscription: %s", path, recorder.Body.String())
		}
	}
}

func TestSubHandlerLinkDisableReturns404ForBaseSubscription(t *testing.T) {
	initSubTestDB(t)
	resetRateLimitBucketsForTest()
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(model.Setting{}).Where("key = ?", "subLinkEnable").Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{}`),
		Inbounds:  json.RawMessage(`[]`),
		Links:     json.RawMessage(`[]`),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewSubHandler(router.Group("/sub"))

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/sub/secret-id", nil)
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s base subscription should be hidden, got %d", method, recorder.Code)
		}
	}

	jsonRecorder := httptest.NewRecorder()
	router.ServeHTTP(jsonRecorder, httptest.NewRequest(http.MethodGet, "/sub/secret-id?format=json", nil))
	if jsonRecorder.Code != http.StatusOK {
		t.Fatalf("json format should use subJsonEnable, got %d", jsonRecorder.Code)
	}
}

func TestJsonSubscriptionKeepsRemoteOutboundsAfterClientOutbounds(t *testing.T) {
	initSubTestDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()

	inbound := model.Inbound{
		Type:    "vless",
		Tag:     "local-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"local-node","server":"local.example.com","server_port":443}`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}

	subscription := model.RemoteOutboundSubscription{Name: "Remote", Url: "https://example.com/sub", Enabled: true}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	group := model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client", Enabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Remote Node",
		SourceKey:      "remote-node",
		Type:           "vless",
		OutboundTag:    "remote-node",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"remote.example.com","server_port":443,"uuid":"22222222-2222-4222-8222-222222222222"}`),
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.RemoteOutboundGroupConnection{GroupId: group.Id, ConnectionId: connection.Id}).Error; err != nil {
		t.Fatal(err)
	}

	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`),
		Inbounds:  json.RawMessage(`[` + strconv.FormatUint(uint64(inbound.Id), 10) + `]`),
		Links:     json.RawMessage(`[{ "type": "remoteGroup", "groupId": ` + strconv.FormatUint(uint64(group.Id), 10) + ` }]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := (&JsonService{}).GetJson("secret-id", "")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}
	selectorRefs := jsonSelectorOutbounds(t, config, "proxy")
	localIndex := indexOfString(selectorRefs, "local-node")
	remoteIndex := indexOfString(selectorRefs, "remote-node")
	if localIndex < 0 || remoteIndex < 0 {
		t.Fatalf("selector does not contain expected tags: %#v", selectorRefs)
	}
	if remoteIndex <= localIndex {
		t.Fatalf("remote outbound should follow client outbound, selector=%#v", selectorRefs)
	}
}

func jsonSelectorOutbounds(t *testing.T, config map[string]interface{}, tag string) []string {
	t.Helper()
	rawOutbounds, ok := config["outbounds"].([]interface{})
	if !ok {
		t.Fatalf("config has no outbounds array: %#v", config["outbounds"])
	}
	for _, raw := range rawOutbounds {
		outbound, ok := raw.(map[string]interface{})
		if !ok || outbound["tag"] != tag {
			continue
		}
		rawRefs, ok := outbound["outbounds"].([]interface{})
		if !ok {
			t.Fatalf("selector %q has no outbounds: %#v", tag, outbound)
		}
		refs := make([]string, 0, len(rawRefs))
		for _, rawRef := range rawRefs {
			if ref, ok := rawRef.(string); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	}
	t.Fatalf("selector %q not found in %#v", tag, rawOutbounds)
	return nil
}

func indexOfString(values []string, needle string) int {
	for index, value := range values {
		if value == needle {
			return index
		}
	}
	return -1
}
