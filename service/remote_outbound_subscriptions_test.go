package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestOutboundDeleteClearsRemoteOutboundLink(t *testing.T) {
	initSettingTestDB(t)
	db := database.GetDB()

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
	connection := model.RemoteOutboundConnection{
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
	if err := addRemoteOutboundGroupConnectionTx(db, group.Id, connection.Id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(outbound.Tag)
	if err := (&OutboundService{}).Save(db, "del", payload); err != nil {
		t.Fatal(err)
	}

	var stored model.RemoteOutboundConnection
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
	var storedURLTest model.Outbound
	if err := db.First(&storedURLTest, urltest.Id).Error; err != nil {
		t.Fatal(err)
	}
	var options map[string]interface{}
	if err := json.Unmarshal(storedURLTest.Options, &options); err != nil {
		t.Fatal(err)
	}
	refs, _ := options["outbounds"].([]interface{})
	if len(refs) != 1 || refs[0] != "direct" {
		t.Fatalf("urltest references were not pruned safely: %s", string(storedURLTest.Options))
	}
	var storedGroup model.RemoteOutboundGroup
	if err := db.First(&storedGroup, group.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedGroup.OutboundEnabled {
		t.Fatal("remote group outbound sync was not disabled after manual outbound delete")
	}
}

func TestRemoteOutboundUnsyncPrunesSelectorReferences(t *testing.T) {
	initSettingTestDB(t)
	db := database.GetDB()

	subscription, group := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	selector := model.Outbound{Tag: "selector", Type: "selector", Options: json.RawMessage(`{"outbounds":["ros-node"],"default":"ros-node"}`)}
	if err := db.Create(&selector).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
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

	if err := (&RemoteOutboundService{}).unsyncConnectionFromOutboundTx(db, &connection); err != nil {
		t.Fatal(err)
	}

	var storedSelector model.Outbound
	if err := db.First(&storedSelector, selector.Id).Error; err != nil {
		t.Fatal(err)
	}
	var options map[string]interface{}
	if err := json.Unmarshal(storedSelector.Options, &options); err != nil {
		t.Fatal(err)
	}
	refs, _ := options["outbounds"].([]interface{})
	if len(refs) != 1 || refs[0] != "direct" {
		t.Fatalf("selector references were not pruned safely: %s", string(storedSelector.Options))
	}
	if options["default"] != "direct" {
		t.Fatalf("selector default was not moved to fallback: %s", string(storedSelector.Options))
	}
	var deleted int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", outbound.Tag).Count(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("remote outbound was not deleted, count=%d", deleted)
	}
}

func TestRemoteOutboundRetagKeepsUnicodeAndUpdatesLinkedOutbound(t *testing.T) {
	initSettingTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := database.GetDB()

	subscription, group := createRemoteOutboundSubscriptionFixture(t, "Подписка", "старый-")
	outbound := model.Outbound{Tag: "старый-Узел-Москва", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Узел Москва",
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

	_, err := (&RemoteOutboundService{}).SaveSubscription(model.RemoteOutboundSubscription{
		Id:        subscription.Id,
		Name:      subscription.Name,
		Url:       subscription.Url,
		Enabled:   true,
		TagPrefix: "новый-",
	}, true, "test")
	if err != nil {
		t.Fatal(err)
	}

	var storedConnection model.RemoteOutboundConnection
	if err := db.First(&storedConnection, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(storedConnection.OutboundTag) {
		t.Fatalf("tag is not valid utf8: %q", storedConnection.OutboundTag)
	}
	if !strings.HasPrefix(storedConnection.OutboundTag, "новый-") || !strings.Contains(storedConnection.OutboundTag, "Узел-Москва") {
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
	initSettingTestDB(t)
	db := database.GetDB()

	_, err := (&RemoteOutboundService{}).SaveSubscription(model.RemoteOutboundSubscription{
		Name:    "Unsafe",
		Url:     "http://127.0.0.1/sub.txt",
		Enabled: true,
	}, true, "test")
	if err == nil {
		t.Fatal("unsafe subscription URL should be rejected")
	}

	var count int64
	if err := db.Model(&model.RemoteOutboundSubscription{}).Where("name = ?", "Unsafe").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unsafe subscription was persisted, count=%d", count)
	}
}

func TestRemoteOutboundAutoRefreshUsesInternalRefreshWithoutDeadlock(t *testing.T) {
	initSettingTestDB(t)
	db := database.GetDB()
	subscription := model.RemoteOutboundSubscription{
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
		_, err := (&RemoteOutboundService{}).RefreshDueSubscriptions("test")
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

func TestRemoteOutboundGroupsAreManyToManyAndFallbackToDefault(t *testing.T) {
	initSettingTestDB(t)
	db := database.GetDB()
	service := &RemoteOutboundService{}

	subscription, defaultGroup := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	groupA, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client"}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Outbound"}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
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

func TestRemoteOutboundDefaultGroupMembershipCanBeCleared(t *testing.T) {
	initSettingTestDB(t)
	db := database.GetDB()
	service := &RemoteOutboundService{}

	_, defaultGroup := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	connection := model.RemoteOutboundConnection{
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
	if err := addRemoteOutboundGroupConnectionTx(db, defaultGroup.Id, connection.Id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	if err := service.SetGroupConnections(defaultGroup.Id, nil, "test"); err != nil {
		t.Fatal(err)
	}
	assertRemoteGroupMemberships(t, connection.Id)

	var stored model.RemoteOutboundConnection
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
	initSettingTestDB(t)
	db := database.GetDB()
	service := &RemoteOutboundService{}

	subscription, _ := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	groupA, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client"}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Outbound"}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
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

	outbounds, tags, err := service.OutboundsForClientLinks(json.RawMessage(`[
		{"type":"remoteGroup","groupId":` + strconv.FormatUint(uint64(groupA.Id), 10) + `},
		{"type":"remoteGroup","groupId":` + strconv.FormatUint(uint64(groupB.Id), 10) + `}
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
	var stored model.RemoteOutboundConnection
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
	var links []model.RemoteOutboundGroupConnection
	if err := database.GetDB().
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

func createRemoteOutboundSubscriptionFixture(t *testing.T, name string, prefix string) (model.RemoteOutboundSubscription, model.RemoteOutboundGroup) {
	t.Helper()
	db := database.GetDB()
	subscription := model.RemoteOutboundSubscription{
		Name:      name,
		Url:       "https://example.com/sub",
		Enabled:   true,
		TagPrefix: prefix,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	group := model.RemoteOutboundGroup{
		SubscriptionId: subscription.Id,
		Name:           defaultRemoteOutboundGroupName,
		Enabled:        true,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	return subscription, group
}
