//go:build !minimal

package authority

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAuthoritySchemaIsIdempotentAndRestoreReconcilesActiveLikeStates(t *testing.T) {
	db := openAuthorityDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	now := time.Unix(1_800_001_000, 0).UTC()
	for index, state := range []neutral.ReservationState{neutral.ReservationReserved, neutral.ReservationMutationPending, neutral.ReservationActive, neutral.ReservationReleased} {
		reservation := validAuthorityReservation(t, "reservation-"+string(rune('a'+index)), state, now)
		row, err := EncodeReservation(reservation, now.Unix(), now.Unix())
		if err != nil {
			t.Fatalf("encode %s: %v", state, err)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create %s: %v", state, err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return ReconcileRestoredInTx(context.Background(), tx, now) }); err != nil {
		t.Fatalf("reconcile restored: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return ReconcileRestoredInTx(context.Background(), tx, now) }); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	var rows []ReservationModel
	if err := db.Order("reservation_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	for _, row := range rows {
		reservation, err := DecodeReservation(row)
		if err != nil {
			t.Fatalf("decode %s: %v", row.ReservationID, err)
		}
		if strings.HasSuffix(row.ReservationID, "d") {
			if reservation.State != neutral.ReservationReleased {
				t.Fatalf("released state changed: %s", reservation.State)
			}
		} else if reservation.State != neutral.ReservationReconcileRequired || !reservation.Status(now).BlocksMutation {
			t.Fatalf("restored state = %#v", reservation)
		}
	}
}

func TestAuthorityGuardsExpiryAndInvalidRowsFailClosed(t *testing.T) {
	db := openAuthorityDB(t)
	now := time.Unix(1_800_002_000, 0).UTC()
	expiredReserved := validAuthorityReservation(t, "expired-reserved", neutral.ReservationReserved, now.Add(-10*time.Minute))
	expiredActive := validAuthorityReservation(t, "expired-active", neutral.ReservationActive, now.Add(-20*time.Minute))
	for _, reservation := range []neutral.ProviderTargetReservationV1{expiredReserved, expiredActive} {
		row, err := EncodeReservation(reservation, reservation.IssuedAt, reservation.RenewedAt)
		if err != nil {
			t.Fatalf("encode reservation: %v", err)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create reservation: %v", err)
		}
	}
	if err := GuardSiteMutation(db, "fallback-html", "site:1", now); err == nil {
		t.Fatalf("expired ACTIVE did not block mutation")
	}
	if err := db.Where("reservation_id = ?", expiredActive.ReservationID).Delete(&ReservationModel{}).Error; err != nil {
		t.Fatalf("delete active row: %v", err)
	}
	if err := GuardSiteMutation(db, "fallback-html", "site:1", now); err != nil {
		t.Fatalf("expired RESERVED should not block: %v", err)
	}
	invalid := ReservationModel{ReservationID: "invalid-row", Schema: neutral.ProviderTargetReservationSchemaV1, ReservationRevision: "r-invalid", ProviderID: "fallback-html", TargetID: "site:999", HolderID: "holder", Purpose: string(neutral.ReservationPurposeNativeFallback), TargetReferenceJSON: "{}", State: string(neutral.ReservationActive), IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix(), ReasonCodesJSON: "[]", CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatalf("create invalid row: %v", err)
	}
	if err := GuardSiteMutation(db, "fallback-html", "site:1", now); err == nil {
		t.Fatalf("invalid persisted row did not fail closed")
	}
}

func TestRestoreCancellationRollsBack(t *testing.T) {
	db := openAuthorityDB(t)
	now := time.Unix(1_800_003_000, 0).UTC()
	reservation := validAuthorityReservation(t, "cancel-active", neutral.ReservationActive, now)
	row, _ := EncodeReservation(reservation, now.Unix(), now.Unix())
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create reservation: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.Transaction(func(tx *gorm.DB) error { return ReconcileRestoredInTx(ctx, tx, now) }); err == nil {
		t.Fatalf("cancelled restore unexpectedly succeeded")
	}
	var persisted ReservationModel
	if err := db.First(&persisted, "reservation_id = ?", reservation.ReservationID).Error; err != nil {
		t.Fatalf("reload reservation: %v", err)
	}
	decoded, err := DecodeReservation(persisted)
	if err != nil || decoded.State != neutral.ReservationActive || decoded.ReservationRevision != reservation.ReservationRevision {
		t.Fatalf("cancelled restore changed row: %#v, %v", decoded, err)
	}
}

func TestRestoreRejectsGlobalCapacityOverflow(t *testing.T) {
	db := openAuthorityDB(t)
	now := time.Unix(1_800_004_000, 0).UTC()
	rows := make([]ReservationModel, 0, neutral.MaxReservationsV2+1)
	for index := 0; index <= neutral.MaxReservationsV2; index++ {
		reservation := validAuthorityReservation(t, "overflow-"+strconv.Itoa(index), neutral.ReservationReconcileRequired, now)
		reservation.ExactTargetReference.TargetID = "site:" + strconv.Itoa(index+1)
		row, err := EncodeReservation(reservation, now.Unix(), now.Unix())
		if err != nil {
			t.Fatalf("encode overflow reservation %d: %v", index, err)
		}
		rows = append(rows, row)
	}
	if err := db.CreateInBatches(&rows, 256).Error; err != nil {
		t.Fatalf("create overflow authority: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return ReconcileRestoredInTx(context.Background(), tx, now) }); err == nil {
		t.Fatal("restore accepted authority above the global provider limit")
	}
}

func TestReplayRequiresFallbackHTMLReservationAuthority(t *testing.T) {
	now := time.Unix(1_800_005_000, 0).UTC()
	for _, test := range []struct {
		name      string
		foreign   bool
		withStore bool
	}{
		{name: "foreign provider", foreign: true, withStore: true},
		{name: "orphan reservation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openAuthorityDB(t)
			reservation := validAuthorityReservation(t, "replay-reservation", neutral.ReservationReserved, now)
			if test.withStore {
				row, err := EncodeReservation(reservation, now.Unix(), now.Unix())
				if err != nil {
					t.Fatalf("create reservation: %v", err)
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("create reservation: %v", err)
				}
			}
			result := reservation
			if test.foreign {
				result.ExactTargetReference.ProviderID = "foreign-provider"
			}
			payload, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			revision := strings.Repeat("a", 64)
			replay := ReservationReplayModel{RequestID: "replay-request", RequestRevision: revision, ReservationID: result.ReservationID, ResultJSON: string(payload), CreatedAt: now.Unix()}
			if err := db.Create(&replay).Error; err != nil {
				t.Fatalf("create replay: %v", err)
			}
			if _, _, err := LoadReplay(db, replay.RequestID, revision); err == nil {
				t.Fatal("invalid replay was accepted at runtime")
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return ReconcileRestoredInTx(context.Background(), tx, now) }); err == nil {
				t.Fatal("invalid replay was accepted during restore")
			}
		})
	}
}

func TestMutationLockAcquisitionHonorsContextCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- WithMutationLock(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- WithMutationLockContext(ctx, func() error { return nil }) }()
	cancel()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lock cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled lock acquisition remained blocked")
	}
	close(release)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder error: %v", err)
	}
}

func openAuthorityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return db
}

func validAuthorityReservation(t *testing.T, reservationID string, state neutral.ReservationState, now time.Time) neutral.ProviderTargetReservationV1 {
	t.Helper()
	target, err := neutral.FinalizeFallbackTargetV2(neutral.FallbackTargetV2{
		Identity:         neutral.TargetIdentity{ProviderID: "fallback-html", TargetID: "site:1"},
		Publish:          neutral.PublishFactsV2{Revision: "publish-1", ContentDigest: strings.Repeat("a", 64)},
		Endpoint:         neutral.EndpointV2{EndpointID: "endpoint-1", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, Address: "127.0.0.1", Port: 8080, Local: true, TransportSecurity: neutral.TransportSecurityPlaintext, ApplicationProtocols: []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11}, ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: hostresources.CapabilityNo},
		Health:           neutral.HealthV2{Readiness: neutral.ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Capacity:         neutral.CapacityV2{State: neutral.CapacityReady, ReservationSlotsTotal: SlotsPerTarget, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "provider-1", Source: "fallback-html-runtime", ConfidenceBP: 10000,
	})
	if err != nil {
		t.Fatalf("finalize target: %v", err)
	}
	reference, _ := neutral.ReferenceV2FromTarget(target)
	reservation := neutral.ProviderTargetReservationV1{Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: reservationID, ReservationRevision: "revision-" + reservationID, HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, State: state, IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix()}
	if state == neutral.ReservationReleased {
		reservation.ReleasedAt = now.Add(time.Second).Unix()
	}
	if err := reservation.Validate(); err != nil {
		t.Fatalf("valid reservation fixture: %v", err)
	}
	return reservation
}
