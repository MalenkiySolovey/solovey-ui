//go:build !minimal

package telegram_test

import (
	"context"
	"testing"
	"time"

	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

func TestSettingFixtureDrainsAuditBeforeRetiringDatabase(t *testing.T) {
	t.Run("queued_audit", func(t *testing.T) {
		initSettingTestDB(t)
		if err := (&coreservice.AuditService{}).Record(coreservice.AuditEvent{Actor: "fixture", Event: "fixture_teardown", Resource: "fixture"}); err != nil {
			t.Fatal(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := coreservice.StopAuditWriter(ctx); err != nil {
		t.Fatal("fixture left an audit batch bound to a retired SQL pool", err)
	}
}
