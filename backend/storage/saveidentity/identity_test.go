package saveidentity

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type identityRow struct {
	ID uint `gorm:"primaryKey"`
}

func TestValidateCreateAndEditIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identityRow{}); err != nil {
		t.Fatal(err)
	}
	row := identityRow{}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := Validate(db, "new", 0, &identityRow{}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(db, "new", row.ID, &identityRow{}); err == nil {
		t.Fatal("create accepted an existing id")
	}
	if err := Validate(db, "edit", row.ID, &identityRow{}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(db, "edit", row.ID+1, &identityRow{}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing edit error = %v", err)
	}
}
