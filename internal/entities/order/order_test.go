package order

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type orderedRow struct {
	ID        uint `gorm:"primarykey"`
	SortOrder int
	Name      string
}

func newOrderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "CGO_ENABLED=0") {
			t.Skip("sqlite driver requires CGO in this environment")
		}
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&orderedRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestParseIDs(t *testing.T) {
	ids, err := ParseIDs(json.RawMessage(`[3,2,1]`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []uint{3, 2, 1}) {
		t.Fatalf("ids = %#v", ids)
	}
	for _, raw := range []string{`[1,1.5]`, `["bad"]`} {
		if _, err := ParseIDs(json.RawMessage(raw)); err == nil {
			t.Fatalf("ParseIDs(%s) succeeded, want error", raw)
		}
	}
}

func TestValidateIDs(t *testing.T) {
	if err := ValidateIDs([]uint{1, 2, 3}, []uint{3, 2, 1}); err != nil {
		t.Fatalf("valid IDs rejected: %v", err)
	}
	for _, requested := range [][]uint{
		{1, 2},
		{1, 2, 2},
		{1, 2, 9},
	} {
		if err := ValidateIDs([]uint{1, 2, 3}, requested); err == nil {
			t.Fatalf("ValidateIDs(%v) succeeded, want error", requested)
		}
	}
}

func TestForSaveAndReorderDBTarget(t *testing.T) {
	db := newOrderTestDB(t)
	rows := []orderedRow{
		{Name: "one", SortOrder: 1},
		{Name: "two", SortOrder: 2},
		{Name: "three", SortOrder: 3},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	next, err := Next(db, &orderedRow{})
	if err != nil {
		t.Fatal(err)
	}
	if next != 4 {
		t.Fatalf("next = %d, want 4", next)
	}
	existing, err := ForSave(db, &orderedRow{}, rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if existing != 2 {
		t.Fatalf("existing = %d, want 2", existing)
	}

	payload, _ := json.Marshal([]uint{rows[2].ID, rows[0].ID, rows[1].ID})
	if err := ReorderDBTarget(db, DBTarget{ModelValue: &orderedRow{}}, payload); err != nil {
		t.Fatal(err)
	}

	var got []orderedRow
	if err := db.Order(Clause).Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	if !reflect.DeepEqual(names, []string{"three", "one", "two"}) {
		t.Fatalf("order = %#v", names)
	}
}
