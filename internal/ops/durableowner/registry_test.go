package durableowner

import (
	"testing"

	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestCatalogReturnsImmutableCopiesAndRejectsContractDrift(t *testing.T) {
	item := componentmanifest.Manifest{
		ID: "immutable-owner-fixture", Name: "Immutable owner", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess,
		Database: componentmanifest.Database{Tables: []string{"immutable_owner_rows"}},
	}.Normalized()
	Register(item)

	first, ok := Lookup(item.ID)
	if !ok {
		t.Fatal("registered durable owner is absent")
	}
	first.Database.Tables[0] = "mutated_rows"
	second, ok := Lookup(item.ID)
	if !ok || len(second.Database.Tables) != 1 || second.Database.Tables[0] != "immutable_owner_rows" {
		t.Fatalf("caller mutated catalog authority: %#v", second)
	}

	drifted := item
	drifted.Database.Tables = []string{"different_rows"}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate owner contract drift was silently accepted")
		}
	}()
	Register(drifted)
}
