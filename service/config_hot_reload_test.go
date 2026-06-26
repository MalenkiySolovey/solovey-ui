package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func seedConfigBlobForHotReloadTest(t *testing.T, blob json.RawMessage) {
	t.Helper()
	tx := dbsqlite.DB().Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	if err := (&SettingService{}).SaveConfig(tx, blob); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
}

func createHotReloadInbound(t *testing.T, tag string) model.Inbound {
	t.Helper()
	inbound := model.Inbound{
		Type:    "mixed",
		Tag:     tag,
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":0}`),
	}
	if err := dbsqlite.DB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	return inbound
}

func createHotReloadSSMService(t *testing.T, tag string, inboundTag string) model.Service {
	t.Helper()
	service := model.Service{
		Type:    "ssm-api",
		Tag:     tag,
		Options: json.RawMessage(fmt.Sprintf(`{"listen":"127.0.0.1","listen_port":0,"servers":{"/main":%q}}`, inboundTag)),
	}
	if err := dbsqlite.DB().Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	return service
}

func createHotReloadOutbound(t *testing.T, tag string, port int) model.Outbound {
	t.Helper()
	outbound := model.Outbound{
		Type:    "socks",
		Tag:     tag,
		Options: json.RawMessage(fmt.Sprintf(`{"server":"127.0.0.1","server_port":%d}`, port)),
	}
	if err := dbsqlite.DB().Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	return outbound
}

func socksHotReloadPayload(id uint, tag string, port int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"socks","tag":%q,"server":"127.0.0.1","server_port":%d}`, id, tag, port))
}

const hotReloadWireguardKey = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="

func createHotReloadEndpoint(t *testing.T, tag string) model.Endpoint {
	t.Helper()
	endpoint := model.Endpoint{
		Type: "wireguard",
		Tag:  tag,
		Options: json.RawMessage(fmt.Sprintf(
			`{"system":false,"address":["10.0.0.2/32"],"private_key":%q,"peers":[],"mtu":1408}`,
			hotReloadWireguardKey,
		)),
	}
	if err := dbsqlite.DB().Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func endpointHotReloadPayload(id uint, tag string, mtu int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"id":%d,"type":"wireguard","tag":%q,"system":false,"address":["10.0.0.2/32"],"private_key":%q,"peers":[],"mtu":%d}`,
		id,
		tag,
		hotReloadWireguardKey,
		mtu,
	))
}

func createHotReloadService(t *testing.T, tag string) model.Service {
	t.Helper()
	service := model.Service{
		Type:    "derp",
		Tag:     tag,
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":0}`),
	}
	if err := dbsqlite.DB().Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	return service
}

func serviceHotReloadPayload(id uint, tag string, port int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"derp","tag":%q,"listen":"127.0.0.1","listen_port":%d}`, id, tag, port))
}

func newHotReloadConfigService(t *testing.T) (*ConfigService, *recordingConfigCoreObjectApplier, *recordingConfigCoreLifecycle) {
	t.Helper()
	applier := &recordingConfigCoreObjectApplier{}
	lifecycle := &recordingConfigCoreLifecycle{}
	service := NewConfigServiceWithRuntime(NewRuntime(runningCoreForConfigSaveTest(t)))
	service.coreObjectApplier = applier
	service.coreLifecycle = lifecycle
	return service, applier, lifecycle
}

func TestConfigSaveApplyWaitsForInFlightCoreOperation(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true}}`))

	service, _, lifecycle := newHotReloadConfigService(t)
	manager := service.runtime().restart()
	opStarted := make(chan struct{})
	opRelease := make(chan struct{})
	opDone := make(chan error, 1)
	go func() {
		opDone <- manager.Run(func() error {
			close(opStarted)
			<-opRelease
			return nil
		})
	}()
	<-opStarted

	saveDone := make(chan error, 1)
	go func() {
		_, saveErr := service.Save("config", "set", json.RawMessage(`{"log":{"disabled":true,"level":"warn"}}`), "", "admin", "example.com")
		saveDone <- saveErr
	}()

	select {
	case err := <-saveDone:
		t.Fatalf("Save returned while a core operation was in flight: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(opRelease)
	if err := <-opDone; err != nil {
		t.Fatal(err)
	}
	if err := <-saveDone; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lifecycle.calls, []string{"restart"}) {
		t.Fatalf("lifecycle calls = %#v, want delayed full restart", lifecycle.calls)
	}
}

func TestConfigSaveInboundEditHotReloadsAndCascadesSSMService(t *testing.T) {
	initSettingTestDB(t)
	inbound := createHotReloadInbound(t, "ss-managed")
	ssm := createHotReloadSSMService(t, "ssm", "ss-managed")
	service, applier, lifecycle := newHotReloadConfigService(t)

	objs, err := service.Save("inbounds", "edit", json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"mixed","tag":"ss-managed","listen":"127.0.0.1","listen_port":1}`, inbound.Id)), "", "admin", "example.com")
	if err != nil {
		t.Fatal(err)
	}

	wantCalls := []string{
		fmt.Sprintf("restart inbounds:%d", inbound.Id),
		fmt.Sprintf("restart services:%d", ssm.Id),
	}
	if !reflect.DeepEqual(applier.calls, wantCalls) {
		t.Fatalf("inbound edit core calls = %#v, want %#v", applier.calls, wantCalls)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("inbound edit must stay hot, got lifecycle calls %#v", lifecycle.calls)
	}
	if !reflect.DeepEqual(objs, []string{"inbounds", "clients"}) {
		t.Fatalf("unexpected partial reload objects: %#v", objs)
	}
}

func TestConfigSaveInboundDeleteRemovesFromCoreWithoutRestart(t *testing.T) {
	initSettingTestDB(t)
	createHotReloadInbound(t, "inb-hot-del")
	service, applier, lifecycle := newHotReloadConfigService(t)

	if _, err := service.Save("inbounds", "del", json.RawMessage(`"inb-hot-del"`), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(applier.calls, []string{"remove inbounds:inb-hot-del"}) {
		t.Fatalf("inbound delete core calls = %#v", applier.calls)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("inbound delete must stay hot, got lifecycle calls %#v", lifecycle.calls)
	}
	var count int64
	if err := dbsqlite.DB().Model(model.Inbound{}).Where("tag = ?", "inb-hot-del").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("inbound row was not deleted")
	}
}

func TestConfigSaveOutboundEditWithLazyRouteReferenceStaysHot(t *testing.T) {
	initSettingTestDB(t)
	outbound := createHotReloadOutbound(t, "proxy-routed", 1080)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"route":{"final":"proxy-routed","rules":[{"network":"tcp","outbound":"proxy-routed"}]}}`))
	service, applier, lifecycle := newHotReloadConfigService(t)

	objs, err := service.Save("outbounds", "edit", socksHotReloadPayload(outbound.Id, "proxy-routed", 1081), "", "admin", "example.com")
	if err != nil {
		t.Fatal(err)
	}

	wantCalls := []string{fmt.Sprintf("restart outbounds:%d", outbound.Id)}
	if !reflect.DeepEqual(applier.calls, wantCalls) {
		t.Fatalf("route-referenced outbound edit core calls = %#v, want %#v", applier.calls, wantCalls)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("route-referenced outbound edit must stay hot, got lifecycle calls %#v", lifecycle.calls)
	}
	if !reflect.DeepEqual(objs, []string{"outbounds"}) {
		t.Fatalf("unexpected partial reload objects: %#v", objs)
	}
}

func TestConfigSaveOutboundEditSelectorMemberRestartsCore(t *testing.T) {
	initSettingTestDB(t)
	member := createHotReloadOutbound(t, "proxy-member", 1080)
	selector := model.Outbound{
		Type:    "selector",
		Tag:     "auto-group",
		Options: json.RawMessage(`{"outbounds":["proxy-member","direct"]}`),
	}
	if err := dbsqlite.DB().Create(&selector).Error; err != nil {
		t.Fatal(err)
	}
	service, applier, lifecycle := newHotReloadConfigService(t)

	if _, err := service.Save("outbounds", "edit", socksHotReloadPayload(member.Id, "proxy-member", 1081), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	if len(applier.calls) != 0 {
		t.Fatalf("selector-member edit must not hot-reload, got calls %#v", applier.calls)
	}
	if !reflect.DeepEqual(lifecycle.calls, []string{"restart"}) {
		t.Fatalf("selector-member edit lifecycle calls = %#v, want full restart", lifecycle.calls)
	}
}

func TestConfigSaveOutboundRenameAndDeleteBlockedByReferences(t *testing.T) {
	initSettingTestDB(t)
	outbound := createHotReloadOutbound(t, "proxy-blocked", 1080)
	selector := model.Outbound{
		Type:    "selector",
		Tag:     "pin-group",
		Options: json.RawMessage(`{"outbounds":["proxy-blocked"]}`),
	}
	if err := dbsqlite.DB().Create(&selector).Error; err != nil {
		t.Fatal(err)
	}
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"route":{"rules":[{"network":"tcp","outbound":"proxy-blocked"}]}}`))

	service := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))
	_, err := service.Save("outbounds", "edit", socksHotReloadPayload(outbound.Id, "proxy-renamed", 1080), "", "admin", "example.com")
	if err == nil {
		t.Fatal("renaming a referenced outbound must be blocked")
	}
	if !strings.Contains(err.Error(), `selector "pin-group" (outbounds list)`) {
		t.Fatalf("rename guard error %q does not name selector reference", err.Error())
	}

	_, err = service.Save("outbounds", "del", json.RawMessage(`"proxy-blocked"`), "", "admin", "example.com")
	if err == nil {
		t.Fatal("deleting a referenced outbound must be blocked")
	}
	for _, fragment := range []string{`outbound "proxy-blocked"`, `selector "pin-group" (outbounds list)`} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("delete guard error %q does not mention %q", err.Error(), fragment)
		}
	}

	var current model.Outbound
	if err := dbsqlite.DB().Model(model.Outbound{}).Where("id = ?", outbound.Id).First(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Tag != "proxy-blocked" {
		t.Fatalf("blocked operations must keep old tag, got %q", current.Tag)
	}
}

func TestConfigSaveEndpointEditWithLazyRouteReferenceStaysHot(t *testing.T) {
	initSettingTestDB(t)
	endpoint := createHotReloadEndpoint(t, "wg-rule-ref")
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"route":{"rules":[{"network":"udp","outbound":"wg-rule-ref"}]}}`))
	service, applier, lifecycle := newHotReloadConfigService(t)

	objs, err := service.Save("endpoints", "edit", endpointHotReloadPayload(endpoint.Id, "wg-rule-ref", 1400), "", "admin", "example.com")
	if err != nil {
		t.Fatal(err)
	}

	wantCalls := []string{fmt.Sprintf("restart endpoints:%d", endpoint.Id)}
	if !reflect.DeepEqual(applier.calls, wantCalls) {
		t.Fatalf("route-referenced endpoint edit core calls = %#v, want %#v", applier.calls, wantCalls)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("route-referenced endpoint edit must stay hot, got lifecycle calls %#v", lifecycle.calls)
	}
	if !reflect.DeepEqual(objs, []string{"endpoints"}) {
		t.Fatalf("unexpected partial reload objects: %#v", objs)
	}
}

func TestConfigSaveEndpointEditWithEagerDetourReferenceRestartsCore(t *testing.T) {
	initSettingTestDB(t)
	endpoint := createHotReloadEndpoint(t, "wg-detour-ref")
	outbound := model.Outbound{
		Type:    "socks",
		Tag:     "socks-via-wg",
		Options: json.RawMessage(`{"server":"127.0.0.1","server_port":1080,"detour":"wg-detour-ref"}`),
	}
	if err := dbsqlite.DB().Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	service, applier, lifecycle := newHotReloadConfigService(t)

	if _, err := service.Save("endpoints", "edit", endpointHotReloadPayload(endpoint.Id, "wg-detour-ref", 1400), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	if len(applier.calls) != 0 {
		t.Fatalf("eagerly referenced endpoint edit must not hot-reload, got calls %#v", applier.calls)
	}
	if !reflect.DeepEqual(lifecycle.calls, []string{"restart"}) {
		t.Fatalf("eagerly referenced endpoint edit lifecycle calls = %#v, want full restart", lifecycle.calls)
	}
}

func TestConfigSaveEndpointRenameAndDeleteBlockedByReferences(t *testing.T) {
	initSettingTestDB(t)
	endpoint := createHotReloadEndpoint(t, "wg-blocked")
	outbound := model.Outbound{
		Type:    "socks",
		Tag:     "socks-pin",
		Options: json.RawMessage(`{"server":"127.0.0.1","server_port":1080,"detour":"wg-blocked"}`),
	}
	if err := dbsqlite.DB().Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"route":{"final":"wg-blocked"}}`))

	service := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))
	_, err := service.Save("endpoints", "edit", endpointHotReloadPayload(endpoint.Id, "wg-renamed", 1408), "", "admin", "example.com")
	if err == nil {
		t.Fatal("renaming a referenced endpoint must be blocked")
	}
	if !strings.Contains(err.Error(), `outbound "socks-pin" (detour)`) {
		t.Fatalf("rename guard error %q does not name outbound detour reference", err.Error())
	}

	_, err = service.Save("endpoints", "del", json.RawMessage(`"wg-blocked"`), "", "admin", "example.com")
	if err == nil {
		t.Fatal("deleting a referenced endpoint must be blocked")
	}
	for _, fragment := range []string{`endpoint "wg-blocked"`, `route final`} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("delete guard error %q does not mention %q", err.Error(), fragment)
		}
	}

	var current model.Endpoint
	if err := dbsqlite.DB().Model(model.Endpoint{}).Where("id = ?", endpoint.Id).First(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Tag != "wg-blocked" {
		t.Fatalf("blocked operations must keep old tag, got %q", current.Tag)
	}
}

func TestConfigSaveEndpointDeleteRemovesFromCoreWithoutRestart(t *testing.T) {
	initSettingTestDB(t)
	createHotReloadEndpoint(t, "wg-hot-del")
	service, applier, lifecycle := newHotReloadConfigService(t)

	if _, err := service.Save("endpoints", "del", json.RawMessage(`"wg-hot-del"`), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(applier.calls, []string{"remove endpoints:wg-hot-del"}) {
		t.Fatalf("endpoint delete core calls = %#v", applier.calls)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("endpoint delete must stay hot, got lifecycle calls %#v", lifecycle.calls)
	}
	var count int64
	if err := dbsqlite.DB().Model(model.Endpoint{}).Where("tag = ?", "wg-hot-del").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("endpoint row was not deleted")
	}
}

func TestConfigSaveServiceEditRenameAndDeleteStayHot(t *testing.T) {
	initSettingTestDB(t)
	serviceRow := createHotReloadService(t, "svc-hot-edit")
	service, applier, lifecycle := newHotReloadConfigService(t)

	if _, err := service.Save("services", "edit", serviceHotReloadPayload(serviceRow.Id, "svc-hot-edit", 1), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save("services", "edit", serviceHotReloadPayload(serviceRow.Id, "svc-new-name", 0), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save("services", "del", json.RawMessage(`"svc-new-name"`), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	wantCalls := []string{
		fmt.Sprintf("restart services:%d", serviceRow.Id),
		"remove services:svc-hot-edit",
		fmt.Sprintf("restart services:%d", serviceRow.Id),
		"remove services:svc-new-name",
	}
	if !reflect.DeepEqual(applier.calls, wantCalls) {
		t.Fatalf("service hot core calls = %#v, want %#v", applier.calls, wantCalls)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("service changes must stay hot, got lifecycle calls %#v", lifecycle.calls)
	}
	var count int64
	if err := dbsqlite.DB().Model(model.Service{}).Where("tag = ?", "svc-new-name").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("service row was not deleted")
	}
}

func TestConfigSaveServiceApplyFailureFallsBackToRestart(t *testing.T) {
	initSettingTestDB(t)
	createHotReloadService(t, "svc-apply-fail")
	service, applier, lifecycle := newHotReloadConfigService(t)
	applier.fail = "remove services:svc-apply-fail"

	if _, err := service.Save("services", "del", json.RawMessage(`"svc-apply-fail"`), "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(applier.calls, []string{"remove services:svc-apply-fail"}) {
		t.Fatalf("service failed apply calls = %#v", applier.calls)
	}
	if !reflect.DeepEqual(lifecycle.calls, []string{"restart"}) {
		t.Fatalf("failed service apply must fall back to restart, got lifecycle calls %#v", lifecycle.calls)
	}
}
