//go:build !minimal

package sub

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gopkg.in/yaml.v3"
)

func initSubRemoteTestDB(t *testing.T) {
	t.Helper()
	initSubTestDB(t)
}

func registerRemoteClientOutboundContributorForTest(t *testing.T) {
	t.Helper()
	unregister := sublocal.RegisterClientOutboundContributor("test.remote", func(ctx sublocal.ClientOutboundContributionContext, set *sublocal.OutboundSet) error {
		var links []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(ctx.RawLinks, &links); err != nil {
			return err
		}
		for _, link := range links {
			if link.Type != "remoteGroup" {
				continue
			}
			set.Append(map[string]interface{}{
				"type":        "vless",
				"tag":         "remote-node",
				"server":      "remote.example.com",
				"server_port": 443,
				"uuid":        "22222222-2222-4222-8222-222222222222",
			}, "remote-node")
		}
		return nil
	})
	t.Cleanup(unregister)
}

func TestJSONSubscriptionDoesNotExpandRemoteClientGroups(t *testing.T) {
	initSubRemoteTestDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()

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

	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`),
		Inbounds:  json.RawMessage(`[` + strconv.FormatUint(uint64(inbound.Id), 10) + `]`),
		Links:     json.RawMessage(`[{ "type": "remoteGroup", "groupId": 1 }]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := (&JSONService{}).GetJSON("secret-id")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}
	selectorRefs := jsonSelectorOutbounds(t, config, "proxy")
	if indexOfString(selectorRefs, "local-node") < 0 {
		t.Fatalf("selector does not contain local client tag: %#v", selectorRefs)
	}
	if indexOfString(selectorRefs, "remote-node") >= 0 {
		t.Fatalf("remote group link must not be expanded into the common client JSON subscription: %#v", selectorRefs)
	}
}

func TestJSONSubscriptionExpandsRemoteClientGroupsWhenContributorRegistered(t *testing.T) {
	initSubRemoteTestDB(t)
	registerRemoteClientOutboundContributorForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()

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

	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`),
		Inbounds:  json.RawMessage(`[` + strconv.FormatUint(uint64(inbound.Id), 10) + `]`),
		Links:     json.RawMessage(`[{ "type": "remoteGroup", "groupId": 1 }]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := (&JSONService{}).GetJSON("secret-id")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}
	selectorRefs := jsonSelectorOutbounds(t, config, "proxy")
	assertContainsInOrder(t, selectorRefs, "local-node", "remote-node")
}

func TestClashSubscriptionDoesNotExpandRemoteClientGroups(t *testing.T) {
	initSubRemoteTestDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()

	inbound := model.Inbound{
		Type:    "vless",
		Tag:     "local-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"local-node","server":"local.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111"}`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}

	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`),
		Inbounds:  json.RawMessage(`[` + strconv.FormatUint(uint64(inbound.Id), 10) + `]`),
		Links:     json.RawMessage(`[{ "type": "remoteGroup", "groupId": 1 }]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := (&ClashService{}).GetClash("secret-id")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}

	proxyNames := clashProxyNames(t, config)
	if indexOfString(proxyNames, "local-node") < 0 {
		t.Fatalf("proxies do not contain local client tag: %#v", proxyNames)
	}
	if indexOfString(proxyNames, "remote-node") >= 0 {
		t.Fatalf("remote group link must not be expanded into the common client Clash subscription: %#v", proxyNames)
	}

	proxyGroupRefs := clashProxyGroupRefs(t, config, "Proxy")
	autoGroupRefs := clashProxyGroupRefs(t, config, "Auto")
	assertContainsInOrder(t, proxyGroupRefs, "Auto", "local-node")
	assertContainsInOrder(t, autoGroupRefs, "local-node")
	if indexOfString(proxyGroupRefs, "remote-node") >= 0 || indexOfString(autoGroupRefs, "remote-node") >= 0 {
		t.Fatalf("remote group link must not be referenced by Clash proxy groups: proxy=%#v auto=%#v", proxyGroupRefs, autoGroupRefs)
	}
}

func TestXraySubscriptionDoesNotExpandRemoteClientGroups(t *testing.T) {
	initSubRemoteTestDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()

	client := model.Client{
		Enable:    true,
		Name:      "alice",
		SubSecret: "secret-id",
		Config:    json.RawMessage(`{}`),
		Inbounds:  json.RawMessage(`[]`),
		Links:     json.RawMessage(`[{ "type": "remoteGroup", "groupId": 1 }]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := (&XrayService{}).GetXray("secret-id")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}
	outboundTags := xrayOutboundTags(t, config)
	if indexOfString(outboundTags, "remote-node") >= 0 || indexOfString(outboundTags, "remote-auto") >= 0 {
		t.Fatalf("remote group link must not be expanded into the common client Xray subscription: %#v", outboundTags)
	}
}

func xrayOutboundTags(t *testing.T, config map[string]interface{}) []string {
	t.Helper()
	rawOutbounds, ok := config["outbounds"].([]interface{})
	if !ok {
		t.Fatalf("xray config has no outbounds array: %#v", config["outbounds"])
	}
	tags := make([]string, 0, len(rawOutbounds))
	for _, raw := range rawOutbounds {
		outbound, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected xray outbound shape: %#v", raw)
		}
		tag, _ := outbound["tag"].(string)
		tags = append(tags, tag)
	}
	return tags
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

func clashProxyNames(t *testing.T, config map[string]interface{}) []string {
	t.Helper()
	rawProxies, ok := config["proxies"].([]interface{})
	if !ok {
		t.Fatalf("clash config has no proxies array: %#v", config["proxies"])
	}
	names := make([]string, 0, len(rawProxies))
	for _, raw := range rawProxies {
		proxy, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected clash proxy shape: %#v", raw)
		}
		name, _ := proxy["name"].(string)
		names = append(names, name)
	}
	return names
}

func clashProxyGroupRefs(t *testing.T, config map[string]interface{}, name string) []string {
	t.Helper()
	rawGroups, ok := config["proxy-groups"].([]interface{})
	if !ok {
		t.Fatalf("clash config has no proxy-groups array: %#v", config["proxy-groups"])
	}
	for _, raw := range rawGroups {
		group, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected clash proxy group shape: %#v", raw)
		}
		if group["name"] != name {
			continue
		}
		rawRefs, ok := group["proxies"].([]interface{})
		if !ok {
			t.Fatalf("proxy group %q has no proxies: %#v", name, group)
		}
		refs := make([]string, 0, len(rawRefs))
		for _, rawRef := range rawRefs {
			if ref, ok := rawRef.(string); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	}
	t.Fatalf("proxy group %q not found in %#v", name, rawGroups)
	return nil
}

func assertContainsInOrder(t *testing.T, values []string, ordered ...string) {
	t.Helper()
	next := 0
	for _, value := range values {
		if next < len(ordered) && value == ordered[next] {
			next++
		}
	}
	if next != len(ordered) {
		t.Fatalf("values do not contain ordered sequence %v: %#v", ordered, values)
	}
}

func indexOfString(values []string, needle string) int {
	for index, value := range values {
		if value == needle {
			return index
		}
	}
	return -1
}
