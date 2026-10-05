package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"sync"
	"testing"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	provider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestRecoveryPreservesHistoricalIdentityAndLegacyPaidEvidence(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		for _, raw := range []string{"", `{"ref":"17","historical":"retained"}`} {
			db := concurrentPaidDB(t)
			client := model.Client{Name: "historical", Volume: 100}
			if err := db.Create(&client).Error; err != nil {
				t.Fatal(err)
			}
			order := paid.PaymentOrder{ClientId: client.Id, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusRecoverable, IdempotencyKey: "historical", GrantSnapshot: snapshot, GrantTraffic: 50, ProviderRef: "17", ProviderPayload: []byte(raw)}
			if err := db.Create(&order).Error; err != nil {
				t.Fatal(err)
			}
			recovery := provider.Reconciliation{OrderID: order.Id, Candidates: []provider.RecoveredInvoice{{Ref: "41", State: provider.InvoicePaid, Verified: true}, {Ref: "17", State: provider.InvoiceActive, Verified: true}}}
			got, err := RecoverProviderOrder(db, recovery, 1000, 86400)
			if err != nil || got.Applied != snapshot {
				t.Fatal("recovery authority wrong", got, err)
			}
			persisted, err := GetOrder(db, order.Id)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ProviderRef != "41" || persisted.ProviderChargeID != "cryptobot:41" || persisted.GrantSnapshot != snapshot {
				t.Fatal("paid identity or snapshot lost")
			}
			if raw != "" && string(persisted.ProviderPayload) != raw {
				t.Fatal("raw history replaced")
			}
			var evidence map[string]string
			if err := json.Unmarshal(persisted.ProviderPayload, &evidence); err != nil || evidence["ref"] != "17" {
				t.Fatal("original reference erased", err)
			}
			queued, err := CancellationWork(db, 100)
			if err != nil || len(queued) != 1 || queued[0].Ref != "17" {
				t.Fatal("active sibling not durable", queued, err)
			}
			if err := CompleteCancellation(db, queued[0].Id); err != nil {
				t.Fatal(err)
			}
			if err := paid.EnsureSchema(db); err != nil {
				t.Fatal(err)
			}
			persisted, err = GetOrder(db, order.Id)
			if err != nil {
				t.Fatal(err)
			}
			if !snapshot && (persisted.Status != paid.StatusManualReview || persisted.LegacyResolved) {
				t.Fatal("paid legacy evidence marked resolved by sibling cancellation")
			}
			if err := db.First(&client, client.Id).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(100)
			if snapshot {
				want = 150
			}
			if client.Volume != want {
				t.Fatal("legacy fabricated grant or modern duplicate", client.Volume)
			}
		}
	}
}

func TestCryptoRecoveryCannotApplyAnotherProviderOrder(t *testing.T) {
	db := concurrentPaidDB(t)
	order := paid.PaymentOrder{Provider: "stars", Status: paid.StatusPending, Amount: 10, Currency: "XTR", GrantSnapshot: true, IdempotencyKey: "other-provider"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	got, err := RecoverProviderOrder(db, provider.Reconciliation{OrderID: order.Id, Candidates: []provider.RecoveredInvoice{{Ref: "41", State: provider.InvoicePaid, Verified: true}}}, 1000, 86400)
	if err != nil || got.Applied {
		t.Fatal("wrong provider entered recovery", got, err)
	}
	persisted, err := GetOrder(db, order.Id)
	if err != nil || persisted.Status != paid.StatusPending || persisted.ProviderChargeID != "" {
		t.Fatal("wrong provider financial state changed", persisted, err)
	}
}

func TestRecoveredPaidGrantAndSiblingQueueCommitTogether(t *testing.T) {
	db := concurrentPaidDB(t)
	client := model.Client{Id: 1, Name: "recovery", Volume: 100, Inbounds: []byte("[]")}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusRecoverable, IdempotencyKey: "recover", GrantSnapshot: true, GrantAddDays: 1, GrantTraffic: 100}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	recovery := provider.Reconciliation{OrderID: order.Id, Candidates: []provider.RecoveredInvoice{{Ref: "41", State: provider.InvoicePaid, Verified: true}, {Ref: "42", State: provider.InvoiceActive, Verified: true}}}
	if err := db.Exec(`CREATE TRIGGER fail_recovery BEFORE UPDATE ON clients BEGIN SELECT RAISE(ABORT,'fixture commit failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverProviderOrder(db, recovery, 1000000, 86400); err == nil {
		t.Fatal("grant failure accepted")
	}
	got, _ := GetOrder(db, order.Id)
	var queues int64
	db.Model(&paid.InvoiceCancellation{}).Count(&queues)
	if got.Status != paid.StatusRecoverable || got.ProviderRef != "" || queues != 0 {
		t.Fatal("partial recovery transaction escaped")
	}
	if err := db.Exec(`DROP TRIGGER fail_recovery`).Error; err != nil {
		t.Fatal(err)
	}
	result, err := RecoverProviderOrder(db, recovery, 1000000, 86400)
	if err != nil || !result.Applied {
		t.Fatal(result, err)
	}
	got, _ = GetOrder(db, order.Id)
	db.Model(&paid.InvoiceCancellation{}).Count(&queues)
	if got.Status != paid.StatusPaid || got.ProviderRef != "41" || got.ProviderChargeID != "cryptobot:41" || queues != 1 {
		t.Fatal("recovery identity/queue missing", got, queues)
	}
	if again, err := RecoverProviderOrder(db, recovery, 1000000, 86400); err != nil || again.Applied {
		t.Fatal("duplicate recovery applied", again, err)
	}
	if err := db.First(&client, 1).Error; err != nil || client.Volume != 200 {
		t.Fatal("recovery did not grant once", err)
	}
}

func TestLegacyRecoveryAndAmbiguityNeverInventPurchase(t *testing.T) {
	for _, kind := range []string{"legacy_paid", "legacy_active", "multiple_paid", "multiple_active", "mismatch", "missing"} {
		t.Run(kind, func(t *testing.T) {
			db := concurrentPaidDB(t)
			client := model.Client{Id: 1, Name: kind, Volume: 100}
			db.Create(&client)
			order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusRecoverable, IdempotencyKey: kind, GrantSnapshot: kind != "legacy_paid" && kind != "legacy_active", GrantTraffic: 100}
			db.Create(&order)
			candidate := provider.RecoveredInvoice{Ref: "41", State: provider.InvoicePaid, Verified: true}
			recovery := provider.Reconciliation{OrderID: order.Id, Candidates: []provider.RecoveredInvoice{candidate}}
			switch kind {
			case "legacy_active":
				recovery.Candidates[0].State = provider.InvoiceActive
			case "multiple_paid":
				candidate.Ref = "42"
				recovery.Candidates = append(recovery.Candidates, candidate)
			case "multiple_active":
				recovery.Candidates[0].State = provider.InvoiceActive
				candidate.Ref = "42"
				candidate.State = provider.InvoiceActive
				recovery.Candidates = append(recovery.Candidates, candidate)
			case "mismatch":
				recovery.Candidates[0].Verified = false
			case "missing":
				recovery.Candidates = nil
			}
			out, err := RecoverProviderOrder(db, recovery, 1000000, 86400)
			if err != nil || out.Applied {
				t.Fatal("unsafe recovery", out, err)
			}
			got, _ := GetOrder(db, order.Id)
			db.First(&client, 1)
			if client.Volume != 100 || got.Status == paid.StatusPaid {
				t.Fatal("ambiguous grant")
			}
			if kind == "legacy_active" {
				work, err := CancellationWork(db, 100)
				if err != nil || len(work) != 1 || got.LegacyResolved {
					t.Fatal("legacy cancellation not durable", work, err)
				}
				if err := CompleteCancellation(db, work[0].Id); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err := paid.EnsureSchema(db); err != nil {
						t.Fatal(err)
					}
				}
				got, _ = GetOrder(db, order.Id)
				if got.Status != paid.StatusFailed || !got.LegacyResolved || got.GrantSnapshot {
					t.Fatal("legacy was fabricated/resurrected", got)
				}
			} else if kind != "missing" && got.Status != paid.StatusManualReview {
				t.Fatal("ambiguity not quarantined", got)
			}
		})
	}
}

func TestRefundClaimArbitratesConcurrentCallsAndRetainsOriginalPolicy(t *testing.T) {
	db := concurrentPaidDB(t)
	client := model.Client{Id: 1, Name: "refund", Volume: 200}
	db.Create(&client)
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "stars", Amount: 10, Currency: "XTR", Status: paid.StatusPaid, TelegramUserId: 7, ProviderChargeID: "tg:original", IdempotencyKey: "refund", GrantSnapshot: true, GrantTraffic: 100}
	db.Create(&order)
	start := make(chan struct{})
	var wg sync.WaitGroup
	tokens := make(chan string, 2)
	failures := make(chan error, 2)
	for _, token := range []string{"one", "two"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			<-start
			_, err := ClaimStarsRefund(db, order.Id, true, token, 100)
			if err == nil {
				tokens <- token
			} else {
				failures <- err
			}
		}(token)
	}
	close(start)
	wg.Wait()
	close(tokens)
	close(failures)
	if len(tokens) != 1 || len(failures) != 1 {
		t.Fatal("refund network claim duplicated")
	}
	first := <-tokens
	if !errors.Is(<-failures, ErrRefundInProgress) {
		t.Fatal("wrong conflict")
	}
	if err := ReleaseRefundClaim(db, order.Id, first); err != nil {
		t.Fatal(err)
	}
	retry, err := ClaimStarsRefund(db, order.Id, false, "retry", 101)
	if err != nil || !retry.RefundRevoke || retry.ProviderChargeID != "tg:original" {
		t.Fatal("retry changed intent", retry, err)
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, first, 102, "test"); !errors.Is(err, ErrOrderAlreadyFinalized) {
		t.Fatal("stale claim completed", err)
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, "retry", 102, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, "retry", 102, "test"); !errors.Is(err, ErrOrderAlreadyFinalized) {
		t.Fatal("refund finalized twice")
	}
	db.First(&client, 1)
	got, _ := GetOrder(db, order.Id)
	if client.Volume != 100 || got.Status != paid.StatusRefunded {
		t.Fatal("refund reversal wrong", client.Volume, got.Status)
	}
}

func TestPersistentProviderCursorRotatesBeyondThousandAndWraps(t *testing.T) {
	db := concurrentPaidDB(t)
	var orders []paid.PaymentOrder
	for id := 1; id <= 1205; id++ {
		orders = append(orders, paid.PaymentOrder{ClientId: uint(id), TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, ProviderRef: fmt.Sprint(id), IdempotencyKey: fmt.Sprint("fair-", id), GrantSnapshot: true})
	}
	if err := db.CreateInBatches(orders, 100).Error; err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 13; batch++ {
		work, err := WorkOrders(db.Session(&gorm.Session{NewDB: true}), "poll", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(work) == 0 {
			t.Fatalf("cursor batch %d returned no work", batch)
		}
		if work[0].Id != uint(batch*100+1) {
			t.Fatal("cursor starvation", batch, work[0].Id)
		}
		if err := AdvanceProviderCursor(db, "poll", work[len(work)-1].Id); err != nil {
			t.Fatal(err)
		}
	}
	work, err := WorkOrders(db, "poll", 100)
	if err != nil || len(work) == 0 || work[0].Id != 1 {
		t.Fatal("cursor did not wrap", err)
	}
	// Remove the cursor's row, then reopen the real file-backed database. The
	// next worker must retain durable progress and wrap despite the missing ID.
	if err := db.Delete(&paid.PaymentOrder{}, 1205).Error; err != nil {
		t.Fatal(err)
	}
	var databaseFile string
	if err := db.Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&databaseFile).Error; err != nil || databaseFile == "" {
		t.Fatal("fixture is not file backed", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := gorm.Open(sqlite.Open(databaseFile+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	restartedPool, err := restarted.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restartedPool.Close() })
	work, err = WorkOrders(restarted, "poll", 100)
	if err != nil || len(work) != 100 || work[0].Id != 1 {
		t.Fatal("restart/deleted cursor prevented wrap", work, err)
	}
	if err := AdvanceProviderCursor(restarted, "poll", work[len(work)-1].Id); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Delete(&paid.PaymentOrder{}, []uint{100, 101}).Error; err != nil {
		t.Fatal(err)
	}
	work, err = WorkOrders(restarted, "poll", 100)
	if err != nil || len(work) != 100 || work[0].Id != 102 {
		t.Fatal("deletion near cursor lost fair progress", work, err)
	}
}

func TestExpiredRefundClaimRetainsIntentAndRejectsOldCompletion(t *testing.T) {
	db := concurrentPaidDB(t)
	if err := db.Create(&model.Client{Id: 1, Name: "restart-refund", Volume: 200}).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "stars", Currency: "XTR", Amount: 10, Status: paid.StatusPaid, TelegramUserId: 7, ProviderChargeID: "tg:original", IdempotencyKey: "restart-refund", GrantSnapshot: true, GrantTraffic: 100}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimStarsRefund(db, order.Id, true, "prior-process", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimStarsRefund(db, order.Id, false, "early-retry", 219); !errors.Is(err, ErrRefundInProgress) {
		t.Fatal("unexpired claim stolen", err)
	}
	if recovered, err := ClaimStarsRefund(db.Session(&gorm.Session{NewDB: true}), order.Id, false, "restarted-process", 221); err != nil || !recovered.RefundRevoke {
		t.Fatal("restart lost original policy", err)
	}
	if err := ReleaseRefundClaim(db, order.Id, "prior-process"); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, "prior-process", 222, "test"); !errors.Is(err, ErrOrderAlreadyFinalized) {
		t.Fatal("stale process committed refund", err)
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, "restarted-process", 222, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestOneProviderChargeCannotGrantTwoOrders(t *testing.T) {
	db := concurrentPaidDB(t)
	for id := 1; id <= 2; id++ {
		if err := db.Create(&model.Client{Id: uint(id), Name: fmt.Sprint("charge-", id), Volume: 100}).Error; err != nil {
			t.Fatal(err)
		}
		order := paid.PaymentOrder{ClientId: uint(id), TariffId: 1, Provider: "stars", Currency: "XTR", Amount: 10, Status: paid.StatusPending, IdempotencyKey: fmt.Sprint("charge-", id), GrantSnapshot: true, GrantTraffic: 100}
		if err := db.Create(&order).Error; err != nil {
			t.Fatal(err)
		}
		result, err := ApplyPaidOrderGrant(db, order.Id, "tg:same-charge", nil, 100, "test")
		if id == 1 && (err != nil || !result.Applied) {
			t.Fatal(result, err)
		}
		if id == 2 && (err == nil || result.Applied) {
			t.Fatal("one charge granted twice", result, err)
		}
	}
	var client model.Client
	db.First(&client, 2)
	if client.Volume != 100 {
		t.Fatal("duplicate charge changed client")
	}
}
