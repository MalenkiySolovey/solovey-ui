package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	remotesub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
	"gorm.io/gorm"
)

func TestOutboundDeleteClearsRemoteOutboundLink(t *testing.T) {
	initSettingTestDB(t)
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
	if err := remotesub.AddGroupConnection(db, group.Id, connection.Id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(outbound.Tag)
	if err := (&OutboundService{}).Save(db, "del", payload); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced outbound delete should be blocked, got %v", err)
	}

	if err := db.Delete(&urltest).Error; err != nil {
		t.Fatal(err)
	}
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
	var storedGroup model.RemoteOutboundGroup
	if err := db.First(&storedGroup, group.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedGroup.OutboundEnabled {
		t.Fatal("remote group outbound sync was not disabled after manual outbound delete")
	}
}

func TestRemoteOutboundUnsyncBlocksReferencedSelector(t *testing.T) {
	initSettingTestDB(t)
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
	initSettingTestDB(t)
	db := dbsqlite.DB()

	_, _, connection, outbound := createSyncedRemoteOutboundFixture(t, "Remote", "ros-", "ros-node")
	payload, _ := json.Marshal(map[string]interface{}{
		"id":          outbound.Id,
		"type":        outbound.Type,
		"tag":         outbound.Tag,
		"server":      "changed.example.com",
		"server_port": 443,
	})
	if err := (&OutboundService{}).Save(db, "edit", payload); err != nil {
		t.Fatal(err)
	}

	var stored model.RemoteOutboundConnection
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Synced || stored.OutboundId != nil {
		t.Fatalf("manual outbound edit did not detach remote link: %#v", stored)
	}

	outbounds, err := (&OutboundService{}).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range *outbounds {
		if item["tag"] != outbound.Tag {
			continue
		}
		if item["remoteOutboundManaged"] == true {
			t.Fatalf("manually edited outbound is still presented as remote-managed: %#v", item)
		}
		return
	}
	t.Fatalf("edited outbound %q not found", outbound.Tag)
}

func TestRemoteOutboundSubscriptionDeleteBlocksReferencedOutbound(t *testing.T) {
	initSettingTestDB(t)
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

	service := &RemoteOutboundService{}
	if err := service.DeleteSubscription(subscription.Id, "test"); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced subscription outbound delete should be blocked, got %v", err)
	}

	var remaining int64
	if err := db.Model(&model.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Count(&remaining).Error; err != nil {
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

	if err := db.Model(&model.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Count(&remaining).Error; err != nil {
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
			config:  `{"dns":{"servers":[{"tag":"dns-remote","address":"1.1.1.1","detour":"ros-node"}]},"route":{"rules":[]}}`,
			locator: `dns server "dns-remote"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initSettingTestDB(t)
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
			config:  `{"dns":{"servers":[{"tag":"dns-remote","address":"1.1.1.1","detour":"ros-node"}]},"route":{"rules":[]}}`,
			locator: `dns server "dns-remote"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initSettingTestDB(t)
			subscription, _, _, _ := createSyncedRemoteOutboundFixture(t, "Remote", "ros-", "ros-node")
			seedRemoteOutboundConfig(t, tc.config)

			err := (&RemoteOutboundService{}).DeleteSubscription(subscription.Id, "test")
			if err == nil {
				t.Fatal("referenced subscription delete should be blocked")
			}
			if !strings.Contains(err.Error(), "still referenced") || !strings.Contains(err.Error(), tc.locator) {
				t.Fatalf("unexpected reference error: %v", err)
			}

			var remaining int64
			if err := dbsqlite.DB().Model(&model.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Count(&remaining).Error; err != nil {
				t.Fatal(err)
			}
			if remaining != 1 {
				t.Fatalf("subscription should remain after blocked delete, count=%d", remaining)
			}
		})
	}
}

func TestRemoteOutboundRetagKeepsUnicodeAndUpdatesLinkedOutbound(t *testing.T) {
	initSettingTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()

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
	db := dbsqlite.DB()

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
	db := dbsqlite.DB()
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
	db := dbsqlite.DB()
	service := &RemoteOutboundService{}

	subscription, defaultGroup := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	groupA, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client", Enabled: true}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Outbound", Enabled: true}, true, "test")
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

func TestRemoteOutboundBulkGroupCreatesMissingGroupPerSubscription(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	service := &RemoteOutboundService{}

	subA, _ := createRemoteOutboundSubscriptionFixture(t, "Remote A", "a-")
	subB, _ := createRemoteOutboundSubscriptionFixture(t, "Remote B", "b-")
	if _, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subA.Id, Name: "Shared", Enabled: true}, true, "test"); err != nil {
		t.Fatal(err)
	}

	result, err := service.SaveGroupForAllSubscriptions(" Shared ", "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "Shared" || result.Created != 1 || result.Skipped != 1 {
		t.Fatalf("bulk result = %#v", result)
	}

	for _, sub := range []model.RemoteOutboundSubscription{subA, subB} {
		var count int64
		if err := db.Model(&model.RemoteOutboundGroup{}).
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
	initSettingTestDB(t)

	_, err := (&RemoteOutboundService{}).SaveGroupForAllSubscriptions("Shared", "test")
	if err == nil || !strings.Contains(err.Error(), "no remote subscriptions") {
		t.Fatalf("empty bulk group should fail clearly, got %v", err)
	}
}

func TestRemoteOutboundDefaultGroupMembershipCanBeCleared(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
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
	if err := remotesub.AddGroupConnection(db, defaultGroup.Id, connection.Id, time.Now().Unix()); err != nil {
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
	db := dbsqlite.DB()
	service := &RemoteOutboundService{}

	subscription, _ := createRemoteOutboundSubscriptionFixture(t, "Remote", "ros-")
	groupA, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Client", Enabled: true}, true, "test")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := service.SaveGroup(model.RemoteOutboundGroup{SubscriptionId: subscription.Id, Name: "Outbound", Enabled: true}, true, "test")
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

func createRemoteOutboundSubscriptionFixture(t *testing.T, name string, prefix string) (model.RemoteOutboundSubscription, model.RemoteOutboundGroup) {
	t.Helper()
	db := dbsqlite.DB()
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

func createSyncedRemoteOutboundFixture(t *testing.T, name string, prefix string, tag string) (model.RemoteOutboundSubscription, model.RemoteOutboundGroup, model.RemoteOutboundConnection, model.Outbound) {
	t.Helper()
	db := dbsqlite.DB()
	subscription, group := createRemoteOutboundSubscriptionFixture(t, name, prefix)
	outbound := model.Outbound{Tag: tag, Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
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
	return subscription, group, connection, outbound
}

func seedRemoteOutboundConfig(t *testing.T, raw string) {
	t.Helper()
	db := dbsqlite.DB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		return (&SettingService{}).SaveConfig(tx, json.RawMessage(raw))
	}); err != nil {
		t.Fatal(err)
	}
}
