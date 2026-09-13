package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	openwrtdeployment "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenWrtNativeFallbackSemanticHistoryIsBoundedAndLatestOnly(t *testing.T) {
	profile := openwrtdeployment.DefaultProfile()
	if err := profile.Validate(); err != nil || profile.DatabaseFolder != openwrtdeployment.DefaultDatabaseFolder {
		t.Fatalf("locked OpenWrt persistent database profile=%#v err=%v", profile, err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "native-native-openwrt.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 12; index++ {
		digest := strings.Repeat(fmt.Sprintf("%x", index%15+1), 64)
		row := NativeFallbackOperationModel{
			Schema: NativeFallbackOperationSchemaV1, OperationID: fmt.Sprintf("openwrt-native-native-%02d", index), Revision: 1,
			ResourceID: "core:inbound:28", InboundDatabaseID: 28, PlanID: digest, PlanDigest: digest, PlanJSON: []byte(`{}`),
			RuntimeIdentityRevision: digest, CapabilityResolverRevision: digest, BeforeConfigurationRevision: digest,
			ExpectedAfterRevision: digest, BeforeEffectiveRevision: digest, TargetReferenceJSON: []byte(`{}`), TargetRevision: digest,
			ProviderRevision: "provider", EndpointRevision: digest, PublishRevision: "publish", HealthRevision: digest,
			CapacityRevision: digest, WorkflowState: NativeWorkflowCancelled, HealthFactsJSON: []byte(`{}`),
			ReasonCodesJSON: []byte(`[]`), RecoveryBundleJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repository := New(db)
	latest, err := repository.LatestNativeFallbackOperations(context.Background(), []string{"core:inbound:28"})
	if err != nil || len(latest) != 1 || latest[0].OperationID != "openwrt-native-native-11" {
		t.Fatalf("OpenWrt latest native operation=%#v err=%v", latest, err)
	}
	result, err := repository.PruneNativeFallbackHistory(context.Background(), 2, 1, 8<<20)
	if err != nil || result.DeletedOperations != 10 || result.DeletedBytes <= 0 {
		t.Fatalf("OpenWrt native retention=%#v err=%v", result, err)
	}
}
