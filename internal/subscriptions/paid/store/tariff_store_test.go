package store

import (
	"encoding/json"
	"fmt"
	"testing"

	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
)

func TestSaveTariffCRUDPreservesZeroValues(t *testing.T) {
	db := newPaidDB(t)
	now := int64(1000)

	if err := SaveTariff(db, "new", json.RawMessage(`{"name":"Month","price":10000,"currency":"RUB","addDays":30,"enabled":true}`), now); err != nil {
		t.Fatalf("new: %v", err)
	}
	all, err := ListTariffs(db)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListTariffs len=%d err=%v", len(all), err)
	}
	id := all[0].Id

	if err := SaveTariff(db, "edit", json.RawMessage(fmt.Sprintf(`{"id":%d,"name":"Month","price":0,"currency":"RUB","addDays":0,"enabled":false}`, id)), now+1); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got, err := GetTariff(db, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 0 || got.Enabled {
		t.Fatalf("zero-valued edit fields not persisted: %#v", got)
	}
	if enabled, err := ListEnabledTariffs(db); err != nil || len(enabled) != 0 {
		t.Fatalf("disabled tariff should not be listed: enabled=%#v err=%v", enabled, err)
	}

	if err := SaveTariff(db, "del", json.RawMessage(fmt.Sprintf(`%d`, id)), now+2); err != nil {
		t.Fatalf("del: %v", err)
	}
	if all, _ := ListTariffs(db); len(all) != 0 {
		t.Fatalf("tariff not deleted: %#v", all)
	}
}

func TestValidateTariffRejectsNegativeValues(t *testing.T) {
	if err := ValidateTariff(&paid.Tariff{Price: -1}); err == nil {
		t.Fatal("negative price accepted")
	}
	if err := ValidateTariff(&paid.Tariff{StarsAmount: 1, AddDays: 0}); err != nil {
		t.Fatalf("valid tariff rejected: %v", err)
	}
}
