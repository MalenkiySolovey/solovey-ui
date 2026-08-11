package coreinboundcontrol

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceLoadsSnapshotsAndExactTLSReferenceCount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:core-inbound-control?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Tls{}, &model.Inbound{}); err != nil {
		t.Fatal(err)
	}
	tlsRecord := model.Tls{Id: 7, Name: "shared", Server: json.RawMessage(`{"enabled":true}`)}
	if err = db.Create(&tlsRecord).Error; err != nil {
		t.Fatal(err)
	}
	for _, inbound := range []model.Inbound{
		{Id: 1, SortOrder: 2, Type: "trojan", Tag: "second", TlsId: 7, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":444,"fallback":{"server":"127.0.0.1","server_port":8080}}`)},
		{Id: 2, SortOrder: 1, Type: "trojan", Tag: "first", TlsId: 7, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":443,"fallback":{"server":"127.0.0.1","server_port":8080}}`)},
	} {
		if err = db.Create(&inbound).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{db: db, identity: exactIdentity(true)}
	snapshots, err := service.ListSnapshots(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].InboundDatabaseID != 2 || snapshots[0].TLSReferenceCount != 2 || snapshots[1].TLSReferenceCount != 2 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	snapshot, err := service.Snapshot(t.Context(), 1)
	if err != nil || snapshot.InboundDatabaseID != 1 || snapshot.TLSReferenceCount != 2 {
		t.Fatalf("snapshot = %#v, err=%v", snapshot, err)
	}
}
