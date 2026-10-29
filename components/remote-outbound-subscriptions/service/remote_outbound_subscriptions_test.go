//go:build !minimal

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	outboundentities "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

func initRemoteOutboundTestDB(t *testing.T) {
	t.Helper()
	initRemoteOutboundTestDBWithHooks(t, true)
}

func newTestRemoteOutboundService() *RemoteOutboundService {
	return &RemoteOutboundService{Runtime: coreservice.NewRuntimeWithCoreProvider(nil)}
}

func TestSourceAutomaticHealthUsesCurrentRuntimeAndPreservesItsPolicy(t *testing.T) {
	initRemoteOutboundTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()
	core := coreruntime.NewCore()
	if err := core.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"health-source-node"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	svc := &RemoteOutboundService{Runtime: coreservice.NewRuntime(core)}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Source-Key") != "" {
			t.Error("source download credentials reached its health target")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	health := func(target string) json.RawMessage {
		value, _ := json.Marshal(remotesub.SourceHealthCheck{Enabled: true, URL: target, Interval: "1m", Timeout: "1s"})
		return value
	}
	saved, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{
		Name: "automatic health", Url: "https://example.com/source", Enabled: true,
		HealthCheck:  health(server.URL + "/?token=private-health-target"),
		FetchHeaders: json.RawMessage(`{"intent":"replace","value":"{\"X-Source-Key\":\"private-download-key\"}"}`),
	}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	outbound := model.Outbound{Tag: "health-source-node", Type: "direct", Options: json.RawMessage(`{}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	for index, enabled := range []bool{true, false, true} {
		connection := remotesub.RemoteOutboundConnection{SubscriptionId: saved.Id, Name: "health member", SourceKey: "health-" + strconv.Itoa(index),
			Type: "direct", OutboundTag: outbound.Tag + strconv.Itoa(index), Enabled: enabled, Options: json.RawMessage(`{}`)}
		if index < 2 {
			connection.OutboundTag, connection.OutboundId, connection.Synced = outbound.Tag, &outbound.Id, true
		}
		// Each stored connection has a unique tag. The disabled entry deliberately
		// has no matching applied outbound and collection-only members never probe.
		if index == 1 {
			connection.OutboundTag += "-disabled"
		}
		if err := db.Create(&connection).Error; err != nil {
			t.Fatal(err)
		}
	}
	schedule := &SourceHealthSchedule{}
	if checked, err := svc.ProbeDueSubscriptionsContext(context.Background(), schedule); err != nil || checked != 1 || requests.Load() != 1 {
		t.Fatal("automatic health did not probe only the active canonical binding", checked, requests.Load(), err)
	}
	observation := core.OutboundHealthSnapshot()[outbound.Tag]
	if observation.Source != coreruntime.ProbeSourceSubscription || observation.Status != "healthy" || observation.TargetID != saved.HealthTargetID {
		t.Fatal("health escaped its canonical runtime owner", observation)
	}
	encoded, _ := json.Marshal(core.OutboundHealthSnapshot())
	if strings.Contains(string(encoded), server.URL) || strings.Contains(string(encoded), "private-health-target") {
		t.Fatal("health projection disclosed a probe target")
	}
	if checked, err := svc.ProbeDueSubscriptionsContext(context.Background(), schedule); err != nil || checked != 0 || requests.Load() != 1 {
		t.Fatal("source interval was ignored", checked, err)
	}
	var original remotesub.RemoteOutboundSubscription
	if err := db.First(&original, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	saved.FetchHeaders, saved.HealthCheck = nil, nil
	if kept, err := svc.SaveSubscription(*saved, true, "test"); err != nil || string(kept.HealthCheck) != string(original.HealthCheck) {
		t.Fatal("omitted policy was silently reset", err)
	}
	for _, raw := range []string{`{"enabled":true,"interval":"1s"}`, `{"enabled":true,"timeout":"16s"}`, `{"enabled":null}`, `{"Enabled":true}`, `{"enabled":true,"enabled":false}`, `{"enabled":true,"url":"file:///private-health-canary"}`, `{"enabled":true,"unknown":"private-health-canary"}`} {
		invalid := *saved
		invalid.HealthCheck = json.RawMessage(raw)
		if _, err := svc.SaveSubscription(invalid, true, "test"); !errors.Is(err, remotesub.ErrSourceHealthCheck) || strings.Contains(err.Error(), "canary") {
			t.Fatal("invalid health policy was accepted or disclosed", err)
		}
	}
	saved.HealthCheck = health(server.URL + "/?token=new-private-target")
	changed, err := svc.SaveSubscription(*saved, true, "test")
	if err != nil || changed.HealthTargetID == observation.TargetID {
		t.Fatal("target change kept old health authority", err)
	}
	var current remotesub.RemoteOutboundSubscription
	if err := db.First(&current, saved.Id).Error; err != nil || !original.SameSource(current) {
		t.Fatal("health policy replaced source collection authority", err)
	}
	if checked, err := svc.ProbeDueSubscriptionsContext(context.Background(), schedule); err != nil || checked != 1 || requests.Load() != 2 {
		t.Fatal("changed probe policy did not become due", checked, err)
	}
	if core.OutboundHealthSnapshot()[outbound.Tag].TargetID != changed.HealthTargetID {
		t.Fatal("new policy did not own the observed target")
	}
	changed.Enabled = false
	changed.FetchHeaders = nil
	if _, err := svc.SaveSubscription(*changed, true, "test"); err != nil {
		t.Fatal(err)
	}
	if checked, err := svc.ProbeDueSubscriptionsContext(context.Background(), schedule); err != nil || checked != 0 || requests.Load() != 2 {
		t.Fatal("disabled source kept probing", checked, err)
	}
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("stopped generation retained health")
	}
	if checked, err := newTestRemoteOutboundService().ProbeDueSubscriptionsContext(context.Background(), schedule); err != nil || checked != 0 {
		t.Fatal("unbound source health used ambient runtime", checked, err)
	}
}

func TestSourceDownloadOutboundUsesStableOwnerAndPreservesUnavailableHistory(t *testing.T) {
	initRemoteOutboundTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()
	svc := newTestRemoteOutboundService()
	outbound := model.Outbound{Tag: "source-download", Type: "direct", Options: json.RawMessage(`{}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	requests, factories, dials := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Source-Key") != "owned-detour-canary" || r.UserAgent() != "detour-source/1.0" {
			t.Error("detour displaced private source headers or User-Agent")
		}
		_, _ = w.Write([]byte(`{"outbounds":[{"type":"shadowsocks","tag":"detour-node","server":"192.0.2.1","server_port":443,"method":"aes-128-gcm","password":"synthetic-password"}]}`))
	}))
	defer server.Close()
	unavailable := false
	svc.OutboundHTTP = componenthost.OutboundHTTPClientFunc(func(tag string, timeout time.Duration) (*http.Client, error) {
		factories++
		if tag != outbound.Tag || timeout != 15*time.Second {
			t.Error("download did not resolve the selected canonical outbound")
		}
		if unavailable {
			return nil, errors.New("runtime stopped")
		}
		return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			dials++
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}}}, nil
	})
	saved, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{
		Name: "selected download", Url: server.URL, UserAgent: "detour-source/1.0", DownloadOutboundID: outbound.Id,
		Enabled: true, AutoUpdate: true, FetchHeaders: json.RawMessage(`{"intent":"replace","value":"{\"X-Source-Key\":\"owned-detour-canary\"}"}`),
	}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	if count, err := svc.RefreshDueSubscriptionsContext(context.Background(), "test"); err != nil || count != 1 || requests != 1 || factories != 1 || dials != 1 {
		t.Fatal("scheduled source download bypassed the selected runtime port", err, count, requests, factories, dials)
	}
	var original remotesub.RemoteOutboundSubscription
	if err := db.First(&original, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	var connection remotesub.RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", saved.Id).First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	unavailable = true
	if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err == nil || requests != 1 {
		t.Fatal("unavailable runtime fell back to host networking", err)
	}
	if err := db.First(&connection, connection.Id).Error; err != nil {
		t.Fatal("failed detour removed the last good collection", err)
	}
	outbound.Tag = "renamed-download"
	if err := db.Save(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	unavailable = false
	if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err != nil || requests != 2 {
		t.Fatal("outbound rename broke the stable source reference", err)
	}
	outbound.Type = "unavailable-future-protocol"
	if err := db.Save(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	choices, err := svc.DownloadOutbounds()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, choice := range choices {
		if choice.ID == outbound.Id {
			found = true
			if choice.Capability.Available {
				t.Fatal("historical unavailable outbound advertised as available")
			}
		}
	}
	if !found {
		t.Fatal("unavailable reference disappeared from inspection facts")
	}
	saved.FetchHeaders = nil // omission retains its row-owned credential
	if _, err := svc.SaveSubscription(*saved, true, "test"); err != nil {
		t.Fatal("source edit destroyed a preserved unavailable reference", err)
	}
	previousFactories := factories
	if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err == nil || factories != previousFactories || requests != 2 {
		t.Fatal("unavailable capability entered runtime download", err)
	}
	var current remotesub.RemoteOutboundSubscription
	if err := db.First(&current, saved.Id).Error; err != nil || !original.SameSource(current) {
		t.Fatal("provider unavailability changed desired source state", err)
	}
	invalid := *saved
	invalid.Id = 0
	if _, err := svc.SaveSubscription(invalid, true, "test"); err == nil {
		t.Fatal("new source selected an unavailable outbound")
	}
	outbound.Type = "direct"
	if err := db.Save(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err != nil || requests != 3 {
		t.Fatal("return to supporting runtime did not reactivate the same source", err)
	}
	payload, _ := json.Marshal(outbound.Tag)
	if err := (&coreservice.OutboundService{}).Save(db, "del", payload); err == nil || !strings.Contains(err.Error(), "SUBSCRIPTION_DOWNLOAD_OUTBOUND_REFERENCED") {
		t.Fatal("referenced download outbound was deleted", err)
	}
	saved.DownloadOutboundID = 0
	if _, err := svc.SaveSubscription(*saved, true, "test"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&current, saved.Id).Error; err != nil || original.SameSource(current) {
		t.Fatal("detour removal retained old refresh authority", err)
	}
	if err := (&coreservice.OutboundService{}).Save(db, "del", payload); err != nil {
		t.Fatal("explicitly cleared detour retained an outbound deletion reference", err)
	}
}

func TestRemoteSourceHeadersStayRowBoundAndReachOnlyTheFetchOwner(t *testing.T) {
	initRemoteOutboundTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()
	svc := newTestRemoteOutboundService()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-header-canary" || strings.Join(r.Header.Values("X-Scopes"), ",") != "one,two" || r.UserAgent() != "source-header-test/1.0" {
			t.Error("private headers or independent User-Agent lost their request owner")
		}
		_, _ = w.Write([]byte(`{"outbounds":[{"type":"shadowsocks","tag":"header-source","server":"192.0.2.1","server_port":443,"method":"aes-128-gcm","password":"synthetic-endpoint-password"}]}`))
	}))
	defer server.Close()
	mutation := func(value string) json.RawMessage {
		raw, _ := json.Marshal(map[string]string{"intent": "replace", "value": value})
		return raw
	}
	saved, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "private-headers", Url: server.URL, UserAgent: "source-header-test/1.0", FetchHeaders: mutation(`{"Authorization":"Bearer private-header-canary","X-Scopes":["one","two"]}`)}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	view, _ := json.Marshal(saved)
	if strings.Contains(string(view), "canary") || strings.Contains(string(view), "sbox:v1:") || !strings.Contains(string(saved.FetchHeaders), `"present":true`) {
		t.Fatal("normal source read disclosed captured HTTP credentials")
	}
	var original remotesub.RemoteOutboundSubscription
	if err := db.First(&original, saved.Id).Error; err != nil || !strings.HasPrefix(original.SourceHeaders, "sbox:v1:") || strings.Contains(original.SourceHeaders, "canary") {
		t.Fatal("request headers were not sealed in their source row", err)
	}
	if result, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err != nil || result.Fetched != 1 {
		t.Fatal("credential-bearing source did not use the normal collection owner", err)
	}
	var keep map[string]any
	_ = json.Unmarshal(saved.FetchHeaders, &keep)
	keep["intent"] = "unchanged"
	delete(keep, "correlation")
	saved.FetchHeaders, _ = json.Marshal(keep)
	saved.Name = "renamed-private-headers"
	if _, err := svc.SaveSubscription(*saved, true, "test"); err != nil {
		t.Fatal("masked source edit could not retain private headers", err)
	}
	for _, value := range []string{`{"Authorization":"a","authorization":"b"}`, `{"Host":"private-canary"}`, `{"X-Key":"private-canary\r\nHost: other"}`, `{"X-Key":[null]}`, `{"X-Key":123}`} {
		invalid := *saved
		invalid.FetchHeaders = mutation(value)
		if _, err := svc.SaveSubscription(invalid, true, "test"); !errors.Is(err, remotesub.ErrSourceHeadersInvalid) || strings.Contains(err.Error(), "canary") {
			t.Fatal("malformed private headers were accepted or disclosed", err)
		}
	}
	var kept remotesub.RemoteOutboundSubscription
	if err := db.First(&kept, saved.Id).Error; err != nil || !original.SameSource(kept) {
		t.Fatal("masked edit or failed save replaced the source credential identity", err)
	}
	other, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "other-source", Url: server.URL}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	other.FetchHeaders = saved.FetchHeaders
	if _, err := svc.SaveSubscription(*other, true, "test"); err == nil {
		t.Fatal("private headers crossed source row ownership")
	}
	saved.FetchHeaders = json.RawMessage(`{"intent":"clear"}`)
	cleared, err := svc.SaveSubscription(*saved, true, "test")
	if err != nil || strings.Contains(string(cleared.FetchHeaders), `"present":true`) {
		t.Fatal("explicit clear did not release source credentials", err)
	}
	if err := db.First(&kept, saved.Id).Error; err != nil || kept.SourceHeaders != "" || original.SameSource(kept) {
		t.Fatal("cleared headers retained old refresh authority", err)
	}
}

func TestHostFileSourceRefreshPreservesLastGoodCollection(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	svc := newTestRemoteOutboundService()
	path := filepath.Join(t.TempDir(), "source.json")
	const first = `{"outbounds":[{"type":"shadowsocks","tag":"file-ss","server":"192.0.2.1","server_port":443,"method":"aes-128-gcm","password":"private-file-source-canary"}]}`
	write := func(raw string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(first)
	for _, invalid := range []string{"relative.json", "https://source.example/config", `\\host\share\source.json`} {
		if _, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "invalid-file", SourceType: remotesub.SourceFile, SourcePath: invalid}, true, "test"); !errors.Is(err, remotesub.ErrSourceFilePathInvalid) {
			t.Fatal("file source accepted a relative, URL or network path", err)
		}
	}
	saved, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "host-file", SourceType: remotesub.SourceFile, SourcePath: path, Enabled: true, AutoUpdate: true, UpdateInterval: 300}, true, "test")
	if err != nil || saved == nil || saved.SourcePath != path || !saved.AutoUpdate || saved.Url != "" || len(saved.Content) != 0 {
		t.Fatal("host file source did not retain its own refresh policy", err)
	}
	for _, field := range []string{"content", "url", "user-agent", "headers"} {
		invalid := *saved
		switch field {
		case "content":
			invalid.Content = json.RawMessage(`{"intent":"replace","value":"private-file-source-canary"}`)
		case "url":
			invalid.Url = "https://source.example/sub"
		case "user-agent":
			invalid.UserAgent = "private-file-source-canary"
		case "headers":
			invalid.FetchHeaders = json.RawMessage(`{"intent":"replace","value":"{}"}`)
		}
		if _, err := svc.SaveSubscription(invalid, true, "test"); err == nil || strings.Contains(err.Error(), "canary") {
			t.Fatal("host file source accepted another source owner's fields", err)
		}
	}
	if count, err := svc.RefreshDueSubscriptions("test"); err != nil || count != 1 {
		t.Fatal("existing scheduler did not collect host file", count, err)
	}
	all, err := svc.GetAll()
	if err != nil || all == nil || len(*all) != 1 || len((*all)[0].Connections) != 1 || (*all)[0].Connections[0].Name != "file-ss" {
		t.Fatal("host file did not reach the canonical collection", err)
	}
	loaded := (*all)[0]
	connectionID := loaded.Connections[0].Id
	if _, err := svc.SyncConnectionToOutbound(connectionID, "test"); err != nil {
		t.Fatal("file connection could not use the existing outbound owner", err)
	}
	var before remotesub.RemoteOutboundSubscription
	if err := db.First(&before, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"private-file-source-canary", "", strings.Repeat("x", remotesub.MaxInlineSourceBytes+1)} {
		write(raw)
		if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err == nil || strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), path) {
			t.Fatal("invalid file content was accepted or disclosed", err)
		}
		var current remotesub.RemoteOutboundSubscription
		if err := db.First(&current, saved.Id).Error; err != nil || string(current.CanonicalSnapshot) != string(before.CanonicalSnapshot) || current.LastUpdated != before.LastUpdated || current.LastError == "" {
			t.Fatal("failed file refresh replaced last good source state", err)
		}
		var connection remotesub.RemoteOutboundConnection
		if err := db.First(&connection, connectionID).Error; err != nil || !connection.Synced || connection.OutboundId == nil {
			t.Fatal("failed file refresh removed an owned runtime resource", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); !errors.Is(err, remotesub.ErrSourceFileRead) {
		t.Fatal("missing host file was not a private read failure", err)
	}
	write(first)
	if err := db.Model(&before).Update("last_updated", 0).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := svc.RefreshDueSubscriptions("test"); err != nil || count != 1 {
		t.Fatal("restored host file could not resume scheduled collection", count, err)
	}
	all, err = svc.GetAll()
	if err != nil || all == nil || len(*all) != 1 || (*all)[0].LastError != "" || len((*all)[0].Connections) != 1 || (*all)[0].Connections[0].Id != connectionID || !(*all)[0].Connections[0].Synced {
		t.Fatal("recovery changed the stable connection owner", err)
	}
	loaded = (*all)[0]
	replacement := loaded
	replacement.SourcePath = filepath.Join(filepath.Dir(path), "replacement.json")
	if replacement.SameSource(loaded) {
		t.Fatal("old file refresh retained authority over a different path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := remotesub.FetchSourceContext(ctx, db, loaded, remotesub.FetchOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal("file collection ignored owner cancellation", err)
	}
	if err := svc.DeleteSubscription(saved.Id, "test"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != first {
		t.Fatal("deleting a file source modified the host file", err)
	}
}

func TestInlineSourceUsesOwnedContentAndExistingRefreshPipeline(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	svc := newTestRemoteOutboundService()
	const payload = `{"outbounds":[{"type":"shadowsocks","tag":"inline-ss","server":"192.0.2.1","server_port":443,"method":"aes-128-gcm","password":"private-inline-source-canary"}]}`
	mutation := func(value string) json.RawMessage {
		raw, _ := json.Marshal(map[string]string{"intent": "replace", "value": value})
		return raw
	}
	var before int64
	db.Model(&remotesub.RemoteOutboundSubscription{}).Count(&before)
	if _, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "invalid-inline", SourceType: remotesub.SourceInline, Content: mutation("private-invalid-source-canary")}, true, "test"); err == nil || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid inline content was not privately rejected", err)
	}
	var after int64
	db.Model(&remotesub.RemoteOutboundSubscription{}).Count(&after)
	if after != before {
		t.Fatal("rejected source committed a row")
	}
	saved, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "inline", SourceType: remotesub.SourceInline, Enabled: true, AutoUpdate: true, Content: mutation(payload)}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	if saved.SourceType != remotesub.SourceInline || saved.AutoUpdate || saved.Url != "" {
		t.Fatal("inline source acquired a network refresh policy")
	}
	view, _ := json.Marshal(saved)
	if strings.Contains(string(view), "canary") || strings.Contains(string(view), "sbox:v1:") {
		t.Fatal("normal source read disclosed captured content")
	}
	var row remotesub.RemoteOutboundSubscription
	if err := db.First(&row, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(row.SourceContent, "sbox:v1:") || strings.Contains(row.SourceContent, "canary") {
		t.Fatal("inline content was not sealed in its owner row")
	}
	keep := map[string]any{}
	_ = json.Unmarshal(saved.Content, &keep)
	keep["intent"] = "unchanged"
	delete(keep, "correlation")
	if _, ok := keep["rotationRevision"]; !ok {
		keep["rotationRevision"] = ""
	}
	saved.Content, _ = json.Marshal(keep)
	saved.Name = "renamed-inline"
	if _, err := svc.SaveSubscription(*saved, true, "test"); err != nil {
		t.Fatal("masked edit lost source ownership", err)
	}
	var kept remotesub.RemoteOutboundSubscription
	if err := db.First(&kept, saved.Id).Error; err != nil || !row.SameSource(kept) {
		t.Fatal("unchanged editor changed captured source identity", err)
	}
	invalidFilter := *saved
	invalidFilter.Include = "[private-pattern"
	if _, err := svc.SaveSubscription(invalidFilter, true, "test"); !errors.Is(err, remotesub.ErrSourceFilterInvalid) {
		t.Fatal("invalid filter was accepted or disclosed its expression", err)
	}
	saved.Include, saved.Exclude = "^inline", "-test$"
	saved.RemoveEmojis = true
	for _, raw := range []string{`[]`, `{"server":"private-dial-canary"}`, `{"connect_timeout":"private-dial-canary"}`, `{"domain_resolver":{"server":"dns","private-dial-canary":true}}`} {
		invalid := *saved
		invalid.DialerOverride = json.RawMessage(raw)
		if _, err := svc.SaveSubscription(invalid, true, "test"); err == nil || strings.Contains(err.Error(), "canary") {
			t.Fatal("invalid dial policy was accepted or disclosed", err)
		}
	}
	saved.DialerOverride = json.RawMessage(`{"connect_timeout":"3s","tcp_fast_open":false}`)
	saved, err = svc.SaveSubscription(*saved, true, "test")
	if err != nil {
		t.Fatal("valid selection could not use the existing source owner", err)
	}
	var filtered remotesub.RemoteOutboundSubscription
	if err := db.First(&filtered, saved.Id).Error; err != nil || filtered.Include != "^inline" || filtered.Exclude != "-test$" || !filtered.RemoveEmojis || row.SameSource(filtered) || filtered.SourceContent != row.SourceContent || !remotesub.JSONRawEqual(filtered.DialerOverride, saved.DialerOverride) {
		t.Fatal("filter edit lost content or retained stale refresh authority", err)
	}
	result, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test")
	if err != nil || result.Created != 1 {
		t.Fatal("inline source did not use canonical collection", err)
	}
	all, err := svc.GetAll()
	if err != nil || all == nil || len(*all) != 1 || len((*all)[0].Connections) != 1 {
		t.Fatal("inline connection was not collected", err)
	}
	loaded := (*all)[0]
	if _, err := svc.SyncConnectionToOutbound(loaded.Connections[0].Id, "test"); err != nil {
		t.Fatal("collected inline endpoint could not use canonical outbound sync", err)
	}
	var outbound model.Outbound
	if err := db.Where("tag = ?", loaded.Connections[0].OutboundTag).First(&outbound).Error; err != nil || outbound.Type != "shadowsocks" {
		t.Fatal("inline source bypassed or lost the existing outbound owner", err)
	}
	if _, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Id: saved.Id, Name: "changed-kind", SourceType: remotesub.SourceRemote, Url: "https://example.com/sub"}, true, "test"); err == nil {
		t.Fatal("source kind changed without a replacement source workflow")
	}
	changed := row
	changed.SourceContent += "changed"
	if row.SameSource(changed) {
		t.Fatal("replacement content retained old refresh authority")
	}
	foreign, err := svc.SaveSubscription(remotesub.RemoteOutboundSubscription{Name: "other-inline", SourceType: remotesub.SourceInline, Content: mutation(payload)}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	foreign.Content = saved.Content
	if _, err := svc.SaveSubscription(*foreign, true, "test"); err == nil {
		t.Fatal("masked source content crossed owner identity")
	}
}

func TestRemoteOutboundMutationFailsClosedWithoutRuntime(t *testing.T) {
	initRemoteOutboundTestDB(t)

	_, err := (&RemoteOutboundService{}).SaveSubscription(remotesub.RemoteOutboundSubscription{
		Name: "No runtime", Url: "https://example.com/subscription", Enabled: true,
	}, true, "test")
	if err == nil || !strings.Contains(err.Error(), "runtime is unavailable") {
		t.Fatalf("SaveSubscription() error = %v, want unavailable runtime", err)
	}
	var count int64
	if err := dbsqlite.DB().Model(&remotesub.RemoteOutboundSubscription{}).
		Where("name = ?", "No runtime").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("runtime-less mutation persisted %d rows", count)
	}
}

func initRemoteOutboundTestDBWithHooks(t *testing.T, registerHooks bool) {
	t.Helper()
	initSettingTestDB(t)
	if err := remotesub.EnsureSchema(dbsqlite.DB()); err != nil {
		t.Fatalf("remote subscription schema: %v", err)
	}
	if registerHooks {
		registerRemoteOutboundHooksForServiceTest(t)
	}
}

func registerRemoteOutboundHooksForServiceTest(t *testing.T) {
	t.Helper()
	unregisterSave := coreservice.RegisterOutboundSaveHook("test.remote", func(tx *gorm.DB) error {
		_, err := remotesub.ReconcileOutboundLinks(tx)
		return err
	})
	unregisterDelete := outboundentities.RegisterDeleteHook("test.remote", remotesub.UnsyncConnectionsForDeletedOutbound)
	unregisterMetadata := outboundentities.RegisterMetadataAnnotator("test.remote", remotesub.AnnotateManagedOutboundMetadata)
	t.Cleanup(unregisterMetadata)
	t.Cleanup(unregisterDelete)
	t.Cleanup(unregisterSave)
}

func initSettingTestDB(t *testing.T) *coreservice.SettingService {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", tempDir)
	if err := dbsqlite.Init(filepath.Join(tempDir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		if testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
	return &coreservice.SettingService{}
}

func TestOutboundDeleteClearsRemoteOutboundLink(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()

	subscription, group := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	urltest := model.Outbound{Tag: "auto", Type: "urltest", Options: json.RawMessage(`{"outbounds":["ros-node"],"url":"https://www.gstatic.com/generate_204"}`)}
	if err := db.Create(&urltest).Error; err != nil {
		t.Fatal(err)
	}
	group.OutboundEnabled = true
	if err := db.Save(&group).Error; err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Enabled:        true,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := remotesub.AddGroupConnection(db, group.Id, connection.Id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(outbound.Tag)
	if err := (&coreservice.OutboundService{}).Save(db, "del", payload); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced outbound delete should be blocked, got %v", err)
	}

	if err := db.Delete(&urltest).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&coreservice.OutboundService{}).Save(db, "del", payload); err != nil {
		t.Fatal(err)
	}

	var stored remotesub.RemoteOutboundConnection
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Synced || stored.OutboundId != nil {
		t.Fatalf("remote link was not cleared: %#v", stored)
	}
	var outbounds int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", outbound.Tag).Count(&outbounds).Error; err != nil {
		t.Fatal(err)
	}
	if outbounds != 0 {
		t.Fatalf("outbound was not deleted, count=%d", outbounds)
	}
	var storedGroup remotesub.RemoteOutboundGroup
	if err := db.First(&storedGroup, group.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedGroup.OutboundEnabled {
		t.Fatal("remote group outbound sync was not disabled after manual outbound delete")
	}
}

func TestRemoteOutboundUnsyncBlocksReferencedSelector(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()

	subscription, group := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	selector := model.Outbound{Tag: "selector", Type: "selector", Options: json.RawMessage(`{"outbounds":["ros-node"],"default":"ros-node"}`)}
	if err := db.Create(&selector).Error; err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Enabled:        true,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}

	if err := remotesub.UnsyncConnectionFromOutbound(db, &connection); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced remote outbound unsync should be blocked, got %v", err)
	}

	selector.Options = json.RawMessage(`{"outbounds":["direct"],"default":"direct"}`)
	if err := db.Save(&selector).Error; err != nil {
		t.Fatal(err)
	}
	if err := remotesub.UnsyncConnectionFromOutbound(db, &connection); err != nil {
		t.Fatal(err)
	}

	var deleted int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", outbound.Tag).Count(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("remote outbound was not deleted, count=%d", deleted)
	}
}

func TestRemoteOutboundManualEditDetachesManagedOutbound(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()

	_, _, connection, outbound := createSyncedRemoteOutboundFixture(t, "Remote", "ros-", "ros-node")
	payload, _ := json.Marshal(map[string]interface{}{
		"id":          outbound.Id,
		"type":        outbound.Type,
		"tag":         outbound.Tag,
		"server":      "changed.example.com",
		"server_port": 443,
	})
	if err := (&coreservice.OutboundService{}).Save(db, "edit", payload); err != nil {
		t.Fatal(err)
	}

	var stored remotesub.RemoteOutboundConnection
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Synced || stored.OutboundId != nil {
		t.Fatalf("manual outbound edit did not detach remote link: %#v", stored)
	}

	outbounds, err := (&coreservice.OutboundService{}).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range *outbounds {
		if item["tag"] != outbound.Tag {
			continue
		}
		if _, ok := item["componentBadges"]; ok {
			t.Fatalf("manually edited outbound is still presented as remote-managed: %#v", item)
		}
		return
	}
	t.Fatalf("edited outbound %q not found", outbound.Tag)
}

func TestRemoteOutboundMetadataIsHiddenWithoutMetadataContributor(t *testing.T) {
	initRemoteOutboundTestDBWithHooks(t, false)
	_, _, _, outbound := createSyncedRemoteOutboundFixture(t, "Remote", "ros-", "ros-node")

	outbounds, err := (&coreservice.OutboundService{}).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range *outbounds {
		if item["tag"] != outbound.Tag {
			continue
		}
		if _, ok := item["componentBadges"]; ok {
			t.Fatalf("uninstalled remote component still annotates outbound metadata: %#v", item)
		}
		if _, ok := item["componentNotice"]; ok {
			t.Fatalf("uninstalled remote component leaked subscription metadata: %#v", item)
		}
		return
	}
	t.Fatalf("remote outbound %q not found", outbound.Tag)
}

func TestRemoteOutboundSubscriptionDeleteBlocksReferencedOutbound(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()

	subscription, group := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	urltest := model.Outbound{Tag: "auto", Type: "urltest", Options: json.RawMessage(`{"outbounds":["ros-node"],"url":"https://www.gstatic.com/generate_204"}`)}
	if err := db.Create(&urltest).Error; err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Enabled:        true,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}

	service := newTestRemoteOutboundService()
	if err := service.DeleteSubscription(subscription.Id, "test"); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced subscription outbound delete should be blocked, got %v", err)
	}

	var remaining int64
	if err := db.Model(&remotesub.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("subscription should remain after blocked delete, count=%d", remaining)
	}
	if err := db.Delete(&urltest).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSubscription(subscription.Id, "test"); err != nil {
		t.Fatal(err)
	}

	if err := db.Model(&remotesub.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("subscription was not deleted, count=%d", remaining)
	}
	if err := db.Model(&model.Outbound{}).Where("tag = ?", outbound.Tag).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("synced outbound was not deleted after references were removed, count=%d", remaining)
	}
}

func TestRemoteOutboundUnsyncBlocksConfigReferences(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		locator string
	}{
		{
			name:    "route final",
			config:  `{"route":{"final":"ros-node","rules":[]},"dns":{"servers":[]}}`,
			locator: "route final",
		},
		{
			name:    "route rule",
			config:  `{"route":{"rules":[{"outbound":"ros-node"}]},"dns":{"servers":[]}}`,
			locator: "route rule #0",
		},
		{
			name:    "dns server detour",
			config:  `{"dns":{"servers":[{"type":"udp","tag":"dns-remote","server":"1.1.1.1","detour":"ros-node"}]},"route":{"rules":[]}}`,
			locator: `dns server "dns-remote"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initRemoteOutboundTestDB(t)
			db := dbsqlite.DB()
			_, _, connection, _ := createSyncedRemoteOutboundFixture(t, "Remote", "ros-", "ros-node")
			seedRemoteOutboundConfig(t, tc.config)

			err := remotesub.UnsyncConnectionFromOutbound(db, &connection)
			if err == nil {
				t.Fatal("referenced remote outbound unsync should be blocked")
			}
			if !strings.Contains(err.Error(), "still referenced") || !strings.Contains(err.Error(), tc.locator) {
				t.Fatalf("unexpected reference error: %v", err)
			}
		})
	}
}

func TestRemoteOutboundSubscriptionDeleteBlocksConfigReferences(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		locator string
	}{
		{
			name:    "route final",
			config:  `{"route":{"final":"ros-node","rules":[]},"dns":{"servers":[]}}`,
			locator: "route final",
		},
		{
			name:    "dns server detour",
			config:  `{"dns":{"servers":[{"type":"udp","tag":"dns-remote","server":"1.1.1.1","detour":"ros-node"}]},"route":{"rules":[]}}`,
			locator: `dns server "dns-remote"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initRemoteOutboundTestDB(t)
			subscription, _, _, _ := createSyncedRemoteOutboundFixture(t, "Remote", "ros-", "ros-node")
			seedRemoteOutboundConfig(t, tc.config)

			err := newTestRemoteOutboundService().DeleteSubscription(subscription.Id, "test")
			if err == nil {
				t.Fatal("referenced subscription delete should be blocked")
			}
			if !strings.Contains(err.Error(), "still referenced") || !strings.Contains(err.Error(), tc.locator) {
				t.Fatalf("unexpected reference error: %v", err)
			}

			var remaining int64
			if err := dbsqlite.DB().Model(&remotesub.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Count(&remaining).Error; err != nil {
				t.Fatal(err)
			}
			if remaining != 1 {
				t.Fatalf("subscription should remain after blocked delete, count=%d", remaining)
			}
		})
	}
}

func TestRemoteOutboundRetagKeepsUnicodeAndUpdatesLinkedOutbound(t *testing.T) {
	initRemoteOutboundTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()

	subscription, group := createRemoteOutboundSubscriptionFixture(t, "РџРѕРґРїРёСЃРєР°", "СЃС‚Р°СЂС‹Р№-")
	outbound := model.Outbound{Tag: "СЃС‚Р°СЂС‹Р№-РЈР·РµР»-РњРѕСЃРєРІР°", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "РЈР·РµР» РњРѕСЃРєРІР°",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Enabled:        true,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}

	_, err := newTestRemoteOutboundService().SaveSubscription(remotesub.RemoteOutboundSubscription{
		Id:        subscription.Id,
		Name:      subscription.Name,
		Url:       subscription.Url,
		UserAgent: "CustomSubscriptionClient/1.0",
		Enabled:   true,
		TagPrefix: "РЅРѕРІС‹Р№-",
	}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	var savedSource remotesub.RemoteOutboundSubscription
	if err := db.First(&savedSource, subscription.Id).Error; err != nil || savedSource.UserAgent != "CustomSubscriptionClient/1.0" {
		t.Fatal("source owner did not persist its client identity", err)
	}

	var storedConnection remotesub.RemoteOutboundConnection
	if err := db.First(&storedConnection, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(storedConnection.OutboundTag) {
		t.Fatalf("tag is not valid utf8: %q", storedConnection.OutboundTag)
	}
	if !strings.HasPrefix(storedConnection.OutboundTag, "РЅРѕРІС‹Р№-") || !strings.Contains(storedConnection.OutboundTag, "РЈР·РµР»-РњРѕСЃРєРІР°") {
		t.Fatalf("unexpected unicode tag: %q", storedConnection.OutboundTag)
	}
	var storedOutbound model.Outbound
	if err := db.First(&storedOutbound, outbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedOutbound.Tag != storedConnection.OutboundTag {
		t.Fatalf("linked outbound tag was not updated: outbound=%q connection=%q", storedOutbound.Tag, storedConnection.OutboundTag)
	}
}

func TestRemoteOutboundSaveRejectsUnsafeURLBeforePersisting(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()

	_, err := newTestRemoteOutboundService().SaveSubscription(remotesub.RemoteOutboundSubscription{
		Name:    "Unsafe",
		Url:     "http://127.0.0.1/sub.txt",
		Enabled: true,
	}, true, "test")
	if err == nil {
		t.Fatal("unsafe subscription URL should be rejected")
	}

	var count int64
	if err := db.Model(&remotesub.RemoteOutboundSubscription{}).Where("name = ?", "Unsafe").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unsafe subscription was persisted, count=%d", count)
	}
}

func TestRemoteOutboundAutoRefreshUsesInternalRefreshWithoutDeadlock(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	subscription := remotesub.RemoteOutboundSubscription{
		Name:           "Auto",
		Url:            "file:///tmp/sub.txt",
		Enabled:        true,
		AutoUpdate:     true,
		UpdateInterval: 60,
		LastUpdated:    0,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := newTestRemoteOutboundService().RefreshDueSubscriptions("test")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("auto refresh deadlocked")
	}
}

func TestRemoteOutboundAutoRefreshStopRetainsTimedOutLifecycleForRetry(t *testing.T) {
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	done := make(chan struct{})
	remoteOutboundAutoMu.Lock()
	previousStop := remoteOutboundAutoStop
	previousDone := remoteOutboundAutoDone
	remoteOutboundAutoStop = cancelWorker
	remoteOutboundAutoDone = done
	remoteOutboundAutoMu.Unlock()
	t.Cleanup(func() {
		cancelWorker()
		remoteOutboundAutoMu.Lock()
		remoteOutboundAutoStop = previousStop
		remoteOutboundAutoDone = previousDone
		remoteOutboundAutoMu.Unlock()
	})

	timedOut, cancel := context.WithCancel(context.Background())
	cancel()
	if err := StopRemoteOutboundAutoRefresh(timedOut); !errors.Is(err, context.Canceled) {
		t.Fatalf("StopRemoteOutboundAutoRefresh error = %v, want context cancellation", err)
	}
	if workerCtx.Err() == nil {
		t.Fatal("timed-out Stop did not cancel the worker")
	}
	remoteOutboundAutoMu.Lock()
	retained := remoteOutboundAutoDone == done && remoteOutboundAutoStop != nil
	remoteOutboundAutoMu.Unlock()
	if !retained {
		t.Fatal("timed-out Stop discarded lifecycle state required for retry")
	}

	close(done)
	if err := StopRemoteOutboundAutoRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	remoteOutboundAutoMu.Lock()
	cleared := remoteOutboundAutoDone == nil && remoteOutboundAutoStop == nil
	remoteOutboundAutoMu.Unlock()
	if !cleared {
		t.Fatal("successful retry retained stopped worker state")
	}
}

func TestRemoteOutboundGroupsAreManyToManyAndFallbackToDefault(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	service := newTestRemoteOutboundService()

	subscription, defaultGroup := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	groupA, err := service.SaveGroup(remotesub.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client", Enabled: true}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := service.SaveGroup(remotesub.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Outbound", Enabled: true}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        defaultGroup.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           "vless",
		OutboundTag:    "ros-node",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"example.com","server_port":443}`),
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}

	if err := service.SetGroupConnections(groupA.Id, []uint{connection.Id}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetGroupConnections(groupB.Id, []uint{connection.Id}, "test"); err != nil {
		t.Fatal(err)
	}
	assertRemoteGroupMemberships(t, connection.Id, groupA.Id, groupB.Id)

	if err := service.DeleteGroup(groupA.Id, "test"); err != nil {
		t.Fatal(err)
	}
	assertRemoteGroupMemberships(t, connection.Id, groupB.Id)

	if err := service.DeleteGroup(groupB.Id, "test"); err != nil {
		t.Fatal(err)
	}
	assertRemoteGroupMemberships(t, connection.Id, defaultGroup.Id)
}

func TestRemoteOutboundBulkGroupCreatesMissingGroupPerSubscription(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	service := newTestRemoteOutboundService()

	subA, _ := createRemoteOutboundSubscriptionFixture(t, "Remote A", "a-")
	subB, _ := createRemoteOutboundSubscriptionFixture(t, "Remote B", "b-")
	if _, err := service.SaveGroup(remotesub.RemoteOutboundGroup{SubscriptionId: subA.Id, Name: "Shared", Enabled: true}, true, "test"); err != nil {
		t.Fatal(err)
	}

	result, err := service.SaveGroupForAllSubscriptions(" Shared ", "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "Shared" || result.Created != 1 || result.Skipped != 1 {
		t.Fatalf("bulk result = %#v", result)
	}

	for _, sub := range []remotesub.RemoteOutboundSubscription{subA, subB} {
		var count int64
		if err := db.Model(&remotesub.RemoteOutboundGroup{}).
			Where("subscription_id = ? AND name = ?", sub.Id, "Shared").
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("subscription %d shared group count = %d, want 1", sub.Id, count)
		}
	}
}

func TestRemoteOutboundBulkGroupRequiresSubscriptions(t *testing.T) {
	initRemoteOutboundTestDB(t)

	_, err := newTestRemoteOutboundService().SaveGroupForAllSubscriptions("Shared", "test")
	if err == nil || !strings.Contains(err.Error(), "no remote subscriptions") {
		t.Fatalf("empty bulk group should fail clearly, got %v", err)
	}
}

func TestRemoteOutboundDefaultGroupMembershipCanBeCleared(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	service := newTestRemoteOutboundService()

	_, defaultGroup := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: defaultGroup.SubscriptionId,
		GroupId:        defaultGroup.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           "vless",
		OutboundTag:    "ros-node",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"example.com","server_port":443}`),
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := remotesub.AddGroupConnection(db, defaultGroup.Id, connection.Id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	if err := service.SetGroupConnections(defaultGroup.Id, nil, "test"); err != nil {
		t.Fatal(err)
	}
	assertRemoteGroupMemberships(t, connection.Id)

	var stored remotesub.RemoteOutboundConnection
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.GroupId != 0 {
		t.Fatalf("legacy group id was not cleared: got %d", stored.GroupId)
	}

	if _, err := service.GetAll(); err != nil {
		t.Fatal(err)
	}
	assertRemoteGroupMemberships(t, connection.Id)
}

func TestRemoteOutboundSharedGroupDoesNotDuplicateOrUnsyncEarly(t *testing.T) {
	initRemoteOutboundTestDB(t)
	db := dbsqlite.DB()
	service := newTestRemoteOutboundService()

	subscription, _ := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	groupA, err := service.SaveGroup(remotesub.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client", Enabled: true}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := service.SaveGroup(remotesub.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Outbound", Enabled: true}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           "vless",
		OutboundTag:    "ros-node",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"example.com","server_port":443}`),
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.SetGroupConnections(groupA.Id, []uint{connection.Id}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetGroupConnections(groupB.Id, []uint{connection.Id}, "test"); err != nil {
		t.Fatal(err)
	}

	outbounds, tags, err := remotesub.OutboundsForClientLinks(dbsqlite.DB(), json.RawMessage(`[
		{"type":"remoteGroup","groupId":`+strconv.FormatUint(uint64(groupA.Id), 10)+`},
		{"type":"remoteGroup","groupId":`+strconv.FormatUint(uint64(groupB.Id), 10)+`}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 1 || len(tags) != 1 || tags[0] != connection.OutboundTag {
		t.Fatalf("expected one deduped outbound, got outbounds=%d tags=%v", len(outbounds), tags)
	}

	if _, err := service.ToggleGroupOutbounds(groupA.Id, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ToggleGroupOutbounds(groupB.Id, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ToggleGroupOutbounds(groupA.Id, "test"); err != nil {
		t.Fatal(err)
	}
	var stored remotesub.RemoteOutboundConnection
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.Synced || stored.OutboundId == nil {
		t.Fatalf("connection was unsynced while another outbound group still uses it: %#v", stored)
	}

	if _, err := service.ToggleGroupOutbounds(groupB.Id, "test"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Synced || stored.OutboundId != nil {
		t.Fatalf("connection stayed synced after all outbound groups were disabled: %#v", stored)
	}
}

func assertRemoteGroupMemberships(t *testing.T, connectionID uint, expected ...uint) {
	t.Helper()
	var links []remotesub.RemoteOutboundGroupConnection
	if err := dbsqlite.DB().
		Where("connection_id = ?", connectionID).
		Order("group_id ASC").
		Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	got := make([]uint, 0, len(links))
	for _, link := range links {
		got = append(got, link.GroupId)
	}
	if len(got) != len(expected) {
		t.Fatalf("memberships mismatch: got=%v expected=%v", got, expected)
	}
	for index := range got {
		if got[index] != expected[index] {
			t.Fatalf("memberships mismatch: got=%v expected=%v", got, expected)
		}
	}
}

func createRemoteOutboundSubscriptionFixture(t *testing.T, name string, prefix string) (remotesub.RemoteOutboundSubscription, remotesub.RemoteOutboundGroup) {
	t.Helper()
	db := dbsqlite.DB()
	subscription := remotesub.RemoteOutboundSubscription{
		Name:      name,
		Url:       "https://example.com/sub",
		Enabled:   true,
		TagPrefix: prefix,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	group := remotesub.RemoteOutboundGroup{
		SubscriptionId: subscription.Id,
		Name:           defaultRemoteOutboundGroupName,
		Enabled:        true,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	return subscription, group
}

func createSyncedRemoteOutboundFixture(t *testing.T, name string, prefix string, tag string) (remotesub.RemoteOutboundSubscription, remotesub.RemoteOutboundGroup, remotesub.RemoteOutboundConnection, model.Outbound) {
	t.Helper()
	db := dbsqlite.DB()
	subscription, group := createRemoteOutboundSubscriptionFixture(t, name, prefix)
	outbound := model.Outbound{Tag: tag, Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	connection := remotesub.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Node",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Enabled:        true,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	return subscription, group, connection, outbound
}

func seedRemoteOutboundConfig(t *testing.T, raw string) {
	t.Helper()
	db := dbsqlite.DB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		return (&coreservice.SettingService{}).SaveConfig(tx, json.RawMessage(raw))
	}); err != nil {
		t.Fatal(err)
	}
}
