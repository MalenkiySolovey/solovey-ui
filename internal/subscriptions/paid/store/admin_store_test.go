package store

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
)

func TestAdminStoreListsBindingsAndOrders(t *testing.T) {
	db := newPaidDB(t)
	client := model.Client{Name: "alice", Desc: "note", Enable: true, Expiry: 1234, Inbounds: json.RawMessage("[]")}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetBinding(db, client.Id, 77, 1000); err != nil {
		t.Fatal(err)
	}
	tariff := paid.Tariff{Name: "Month", Price: 10000, Currency: "RUB"}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{
		ClientId:       client.Id,
		TariffId:       tariff.Id,
		Provider:       string(provider.ProviderStars),
		Amount:         5,
		Currency:       "XTR",
		Status:         paid.StatusPaid,
		TelegramUserId: 77,
		IdempotencyKey: "admin-order",
		CreatedAt:      2000,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	bindings, err := ListBindingRows(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].ClientId != client.Id || bindings[0].TgUserId != 77 || bindings[0].Desc != "note" {
		t.Fatalf("bindings = %#v", bindings)
	}
	exists, err := ClientExists(db, client.Id)
	if err != nil || !exists {
		t.Fatalf("ClientExists = %v, %v", exists, err)
	}

	orders, err := ListOrderRows(db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].ClientName != "alice" || orders[0].Provider != string(provider.ProviderStars) {
		t.Fatalf("orders = %#v", orders)
	}
}

func TestListBindingRowsFollowsClientSortOrder(t *testing.T) {
	db := newPaidDB(t)
	clients := []model.Client{
		{Name: "zulu", SortOrder: 1, Enable: true, Inbounds: json.RawMessage("[]")},
		{Name: "alpha", SortOrder: 2, Enable: true, Inbounds: json.RawMessage("[]")},
	}
	if err := db.Create(&clients).Error; err != nil {
		t.Fatal(err)
	}

	bindings, err := ListBindingRows(db)
	if err != nil {
		t.Fatal(err)
	}

	if len(bindings) != 2 {
		t.Fatalf("bindings len = %d, want 2: %#v", len(bindings), bindings)
	}
	got := []string{bindings[0].Name, bindings[1].Name}
	want := []string{"zulu", "alpha"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("binding order = %v, want %v", got, want)
	}
}
