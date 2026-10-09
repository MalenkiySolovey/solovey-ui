package entityclients

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"gorm.io/gorm"
)

func TestPublicRemarkPersistsThroughSaveRenamePartialAndClear(t *testing.T) {
	db := newClientDB(t)
	client := model.Client{Name: "private-auth", Desc: "private-description", Enable: true, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{"trojan":{"password":"fixture"},"_subscription":{"publicRemark":"Public 雪"}}`)}
	save := func(action string) {
		t.Helper()
		raw, _ := json.Marshal(client)
		if _, err := Save(SaveRequest{Tx: db, Action: action, Data: raw, Hostname: "unused.example"}); err != nil {
			t.Fatal(err)
		}
		if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	save("new")
	client.Name = "private-renamed"
	save("edit")
	client.Config = json.RawMessage(`{"trojan":{"password":"changed"}}`)
	save("edit")
	if remark, err := canonical.ClientPublicRemark(client.Config); err != nil || remark != "Public 雪" {
		t.Fatal("rename or legacy partial edit lost public metadata")
	}
	if err := ValidateStored(db); err != nil {
		t.Fatal(err)
	}
	client.Config = json.RawMessage(`{"trojan":{"password":"changed"},"_subscription":{}}`)
	save("edit")
	if remark, err := canonical.ClientPublicRemark(client.Config); err != nil || remark != "" {
		t.Fatal("explicit clear was not saved")
	}
	before := append([]byte(nil), client.Config...)
	client.Config = json.RawMessage(`{"_subscription":{"publicRemark":17}}`)
	raw, _ := json.Marshal(client)
	if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: raw}); err == nil {
		t.Fatal("invalid metadata was saved")
	}
	var retained model.Client
	if err := db.First(&retained, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if string(retained.Config) != string(before) {
		t.Fatal("failed edit changed source")
	}
}

func TestBulkEditRebuildsPublicRemarkWithoutInboundChanges(t *testing.T) {
	db := newClientDB(t)
	inbound := model.Inbound{Type: "trojan", Tag: "inbound-label", Options: json.RawMessage(`{"listen_port":443}`), OutJson: json.RawMessage(`{"type":"trojan"}`), Addrs: json.RawMessage(`[{"server":"example.com","server_port":443}]`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "private-auth", Desc: "private-description", Enable: true, Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{"trojan":{"password":"fixture"}}`)}
	raw, _ := json.Marshal(client)
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: raw, Hostname: "unused.example"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	identity := client.SubSecret
	client.Config = json.RawMessage(`{"trojan":{"password":"fixture"},"_subscription":{"publicRemark":"Public label"}}`)
	raw, _ = json.Marshal([]model.Client{client})
	ids, err := Save(SaveRequest{Tx: db, Action: "editbulk", Data: raw, Hostname: "unused.example", SaveBatch: func(tx *gorm.DB, slice any) error { return tx.Save(slice).Error }})
	if err != nil || len(ids) != 1 || ids[0] != inbound.Id {
		t.Fatalf("config edit did not identify its affected inbound: %v", err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var links []Link
	if err := json.Unmarshal(client.Links, &links); err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatal("bulk edit lost the local link")
	}
	projected, err := url.Parse(LinkString(links[0], "uri"))
	if err != nil {
		t.Fatal("bulk edit produced an invalid URI")
	}
	if projected.Fragment != "Public label" || LinkString(links[0], "remark") != inbound.Tag {
		t.Fatalf("public label=%q, link owner=%q", projected.Fragment, LinkString(links[0], "remark"))
	}
	if strings.Contains(LinkString(links[0], "uri"), "private-description") || client.SubSecret != identity {
		t.Fatal("same-inbound edit leaked private description or changed private identity")
	}
	client.Config, client.Links = nil, nil // current list/bulk editor omits both
	client.Desc = "changed private description"
	raw, _ = json.Marshal([]model.Client{client})
	if _, err := Save(SaveRequest{Tx: db, Action: "editbulk", Data: raw, Hostname: "unused.example", SaveBatch: func(tx *gorm.DB, slice any) error { return tx.Save(slice).Error }}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(client.Config), "fixture") {
		t.Fatal("partial bulk edit lost its protocol credentials")
	}
	if remark, err := canonical.ClientPublicRemark(client.Config); err != nil || remark != "Public label" || client.SubSecret != identity {
		t.Fatal("partial bulk edit lost public metadata or private identity")
	}
}

func TestBulkAttachmentRetainsPreparedSnellKey(t *testing.T) {
	db := newClientDB(t)
	inbound := model.Inbound{Type: "snell", Tag: "snell-bulk", Options: json.RawMessage(`{"version":6}`), OutJson: json.RawMessage(`{"type":"snell","version":6}`), Addrs: json.RawMessage(`[]`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "private-auth", Enable: true, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{"trojan":{"password":"fixture"},"_subscription":{"publicRemark":"Public"}}`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	client.Config, client.Links = nil, nil
	client.Inbounds = json.RawMessage(`[1]`)
	raw, _ := json.Marshal([]model.Client{client})
	if _, err := Save(SaveRequest{Tx: db, Action: "editbulk", Data: raw, Hostname: "unused.example", SaveBatch: func(tx *gorm.DB, slice any) error { return tx.Save(slice).Error }}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	key := snellKey(t, client)
	if len(key) != 32 || !strings.Contains(string(client.Config), "fixture") {
		t.Fatal("late bulk hydration replaced the prepared key or another protocol")
	}
	client.Config, client.Links = nil, nil
	raw, _ = json.Marshal([]model.Client{client})
	if _, err := Save(SaveRequest{Tx: db, Action: "editbulk", Data: raw, Hostname: "unused.example", SaveBatch: func(tx *gorm.DB, slice any) error { return tx.Save(slice).Error }}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if snellKey(t, client) != key {
		t.Fatal("repeated partial bulk edit rotated the existing Snell key")
	}
}

func TestUnsupportedURIDeliveryDoesNotBlockManagedClientSave(t *testing.T) {
	db := newClientDB(t)
	inbound := model.Inbound{Type: "vless", Tag: "fixture-ws", Options: json.RawMessage(`{"listen_port":443,"transport":{"type":"ws","path":"/","max_early_data":1024,"early_data_header_name":"X-Custom"}}`), OutJson: json.RawMessage(`{"type":"vless","tag":"fixture-ws"}`), Addrs: json.RawMessage(`[{"server":"example.com","server_port":443}]`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "private-auth", Desc: "private-description", Enable: true, Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`)}
	raw, _ := json.Marshal(client)
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: raw, Hostname: "unused.example"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	var links []Link
	if err := json.Unmarshal(client.Links, &links); err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || LinkString(links[0], "uri") != "" || !strings.Contains(LinkString(links[0], "diagnostic"), "Sec-WebSocket-Protocol") {
		t.Fatal("valid runtime config lost the visible URI delivery diagnosis")
	}
}
