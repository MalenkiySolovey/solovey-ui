package service

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestClientSaveDeleteBulkReturnsUniqueInboundIDs(t *testing.T) {
	initSettingTestDB(t)
	clients := []model.Client{
		{Name: "delete-a", Inbounds: json.RawMessage(`[1,2]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		{Name: "delete-b", Inbounds: json.RawMessage(`[2,3]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
	}
	if err := dbsqlite.DB().Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]uint{clients[0].Id, clients[1].Id})
	if err != nil {
		t.Fatal(err)
	}

	inboundIDs, err := (&ClientService{}).Save(dbsqlite.DB(), "delbulk", payload, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(inboundIDs, func(i, j int) bool { return inboundIDs[i] < inboundIDs[j] })
	if !reflect.DeepEqual(inboundIDs, []uint{1, 2, 3}) {
		t.Fatalf("inbound IDs = %#v, want [1 2 3]", inboundIDs)
	}

	var count int64
	if err := dbsqlite.DB().Model(model.Client{}).Where("id IN ?", []uint{clients[0].Id, clients[1].Id}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleted clients remaining count = %d", count)
	}
}
