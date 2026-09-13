package serverprotection

import (
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/gorm"
	"strings"
	"testing"
)

// SeedSysupgradeDurableFacts keeps component-specific fixture construction in
// test composition, outside the deployment owner's production boundary.
func SeedSysupgradeDurableFacts(t testing.TB, db *gorm.DB, scenario string) {
	t.Helper()
	if err := db.Create(&repository.IPAllowlistModel{IPCIDR: "192.0.2.10/32", Reason: "management fixture", CreatedBy: "fixture"}).Error; err != nil {
		t.Fatal(err)
	}
	if scenario == "rolled_back" {
		if err := db.Create(&repository.OperationLockModel{OperationID: "rolled-back-before-firmware", Kind: "firewall", State: "rolled_back", Revision: 5}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "active_import_fenced" {
		if err := db.Create(&repository.FirewallCompositionModel{ID: 1, Schema: "fixture", State: "ACTIVE", BindingsJSON: []byte("[]"), Revision: strings.Repeat("a", 64), AppliedOperationID: "pre-firmware"}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func AssertSysupgradeDurableFacts(t testing.TB, db *gorm.DB, scenario string) {
	t.Helper()
	var management int64
	if err := db.Model(&repository.IPAllowlistModel{}).Where("ip_cidr=?", "192.0.2.10/32").Count(&management).Error; err != nil || management != 1 {
		t.Fatal("trusted-management policy lost")
	}
	var composition repository.FirewallCompositionModel
	query := db.Limit(1).Find(&composition)
	if query.Error != nil {
		t.Fatal(query.Error)
	}
	if scenario == "active_import_fenced" {
		if composition.State != "RECOVERY_REQUIRED" {
			t.Fatal("restore authorized pre-firmware active authority")
		}
	} else if query.RowsAffected != 0 {
		t.Fatal("restore resurrected firewall composition")
	}
	if scenario == "rolled_back" {
		var operation repository.OperationLockModel
		if err := db.Where("operation_id=?", "rolled-back-before-firmware").First(&operation).Error; err != nil || operation.State != "rolled_back" {
			t.Fatal("terminal operation history changed")
		}
	}
}
