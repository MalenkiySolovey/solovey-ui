//go:build !minimal

package fallbackhtml

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var _ neutral.ProviderV2 = targetProvider{}

func TestTargetProviderV2PublishesExactProtocolFactsAndRevisions(t *testing.T) {
	fixed := time.Unix(1_800_000_000, 0).UTC()
	for _, test := range []struct {
		name      string
		tls       bool
		wantMode  neutral.TransportSecurity
		protocols []neutral.ApplicationProtocol
		names     []string
	}{
		{name: "plaintext", wantMode: neutral.TransportSecurityPlaintext, protocols: []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11}},
		{name: "tls", tls: true, wantMode: neutral.TransportSecurityTLS, protocols: []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11, neutral.ApplicationProtocolHTTP2}, names: []string{"decoy.example"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openProviderDB(t, test.name)
			site, target := createProviderFixture(t, db, test.tls, "decoy.example", 18443)
			provider := targetProvider{
				db: db, now: func() time.Time { return fixed },
				tlsServerNameVerified: func(name string) bool { return name == "decoy.example" || name == "secure.example" },
			}
			inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
			if providerErr != nil || len(inventory.Targets) != 1 {
				t.Fatalf("InventoryV2 = %#v, %v", inventory, providerErr)
			}
			got := inventory.Targets[0]
			if err := got.Validate(); err != nil {
				t.Fatalf("target validation: %v", err)
			}
			if got.Identity.TargetID != "site:"+providerTestUintString(site.ID) || got.Endpoint.TransportSecurity != test.wantMode || got.Endpoint.Address != "127.0.0.1" || got.Endpoint.Port != uint16(target.Port) {
				t.Fatalf("unexpected target facts: %#v", got)
			}
			if !equalProtocols(got.Endpoint.ApplicationProtocols, test.protocols) || strings.Join(got.Endpoint.AcceptedServerNames, ",") != strings.Join(test.names, ",") {
				t.Fatalf("protocol/name facts = %#v / %#v", got.Endpoint.ApplicationProtocols, got.Endpoint.AcceptedServerNames)
			}
			if got.Health.Readiness != neutral.ReadinessUnknown || got.Capacity.State != neutral.CapacityReady || got.Capacity.ReservationSlotsTotal != authority.SlotsPerTarget {
				t.Fatalf("health/capacity facts = %#v / %#v", got.Health, got.Capacity)
			}
			if neutral.EffectiveReadinessV2(got.Health, time.Unix(got.Health.ExpiresAt+1, 0)) != neutral.ReadinessUnknown || neutral.EffectiveCapacityStateV2(got.Capacity, time.Unix(got.Capacity.ExpiresAt+1, 0)) != neutral.CapacityStale {
				t.Fatalf("provider freshness state is incorrect")
			}
			encoded, _ := json.Marshal(got)
			for _, forbidden := range []string{"://", "C:\\\\", "filePath", "rootDir", "privateKey", "certificate", "secret"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("target leaked forbidden value %q: %s", forbidden, encoded)
				}
			}

			before := got.CanonicalTargetRevision
			if err := db.Model(&fallbackdomain.RuntimeTarget{}).Where("id = ?", target.ID).Update("port", target.Port+1).Error; err != nil {
				t.Fatalf("change port: %v", err)
			}
			changed, changedErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
			if changedErr != nil || len(changed.Targets) != 1 || changed.Targets[0].CanonicalTargetRevision == before || changed.Targets[0].Endpoint.EndpointRevision == got.Endpoint.EndpointRevision {
				t.Fatalf("listener change did not revise target: %#v, %v", changed, changedErr)
			}
			if !test.tls {
				if err := db.Model(&fallbackdomain.RuntimeTarget{}).Where("id = ?", target.ID).Updates(map[string]any{"tls": true, "host": "secure.example"}).Error; err != nil {
					t.Fatalf("change TLS facts: %v", err)
				}
				secure, secureErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
				if secureErr != nil || len(secure.Targets) != 1 || secure.Targets[0].Endpoint.TransportSecurity != neutral.TransportSecurityTLS || secure.Targets[0].CanonicalTargetRevision == changed.Targets[0].CanonicalTargetRevision {
					t.Fatalf("TLS/server-name change did not revise target: %#v, %v", secure, secureErr)
				}
			}
		})
	}

	t.Run("unverified TLS name is not advertised", func(t *testing.T) {
		db := openProviderDB(t, "tls-name-unverified")
		createProviderFixture(t, db, true, "decoy.example", 18443)
		provider := targetProvider{db: db, now: func() time.Time { return fixed }, tlsServerNameVerified: func(string) bool { return false }}
		inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
		if providerErr != nil || len(inventory.Targets) != 0 || !strings.Contains(strings.Join(inventory.ReasonCodes, ","), "tls_server_name_unverified") {
			t.Fatalf("unverified TLS inventory = %#v, %v", inventory, providerErr)
		}
	})
}

func TestTargetProviderV2ClassifiesOnlyEffectivePublishedRedirectsAsManagementIsolated(t *testing.T) {
	fixed := time.Unix(1_800_000_050, 0).UTC()

	t.Run("no redirect remains isolated", func(t *testing.T) {
		db := openProviderDB(t, "redirect-none")
		createProviderFixture(t, db, false, "", 18443)
		inventory, providerErr := (targetProvider{db: db, now: func() time.Time { return fixed }}).
			InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
		if providerErr != nil || len(inventory.Targets) != 1 ||
			inventory.Targets[0].Endpoint.CanReachManagement != hostresources.CapabilityNo {
			t.Fatalf("no-redirect inventory = %#v, %v", inventory, providerErr)
		}
	})

	t.Run("provider local relative redirect remains isolated and revises the target", func(t *testing.T) {
		db := openProviderDB(t, "redirect-local")
		site, _ := createProviderFixture(t, db, false, "", 18443)
		provider := targetProvider{db: db, now: func() time.Time { return fixed }}
		before := providerTargetFromInventory(t, provider)
		beforeReference, err := neutral.ReferenceV2FromTarget(before)
		if err != nil {
			t.Fatalf("before reference: %v", err)
		}
		publish := activeProviderPublish(t, db, site.ID)
		redirect := fallbackdomain.PublishRedirect{
			PublishID: publish.ID, FromPath: "/go/", ToPath: "/about/", StatusCode: 302,
		}
		if err := db.Create(&redirect).Error; err != nil {
			t.Fatalf("create local redirect: %v", err)
		}
		after := providerTargetFromInventory(t, provider)
		if after.Endpoint.CanReachManagement != hostresources.CapabilityNo ||
			after.Publish.ContentDigest == before.Publish.ContentDigest ||
			after.CanonicalTargetRevision == before.CanonicalTargetRevision {
			t.Fatalf("local redirect target was not safely revision-bound: before=%#v after=%#v", before, after)
		}
		if _, providerErr := provider.ResolveV2(context.Background(), beforeReference); providerErr == nil {
			t.Fatal("pre-redirect exact reference remained current")
		}
	})

	t.Run("draft and inactive redirects do not affect the active target", func(t *testing.T) {
		db := openProviderDB(t, "redirect-inactive")
		site, _ := createProviderFixture(t, db, false, "", 18443)
		provider := targetProvider{db: db, now: func() time.Time { return fixed }}
		before := providerTargetFromInventory(t, provider)
		if err := db.Create(&fallbackdomain.Redirect{
			SiteID: site.ID, FromPath: "/draft/", ToPath: "https://draft.example/management", StatusCode: 302, External: true,
		}).Error; err != nil {
			t.Fatalf("create draft redirect: %v", err)
		}
		inactive := fallbackdomain.Publish{SiteID: site.ID, Version: "inactive-publish", RootDir: t.TempDir(), Active: false, CreatedAt: 2}
		if err := db.Create(&inactive).Error; err != nil {
			t.Fatalf("create inactive publish: %v", err)
		}
		if err := db.Create(&fallbackdomain.PublishRedirect{
			PublishID: inactive.ID, FromPath: "/inactive/", ToPath: "https://inactive.example/management", StatusCode: 302, External: true,
		}).Error; err != nil {
			t.Fatalf("create inactive redirect: %v", err)
		}
		after := providerTargetFromInventory(t, provider)
		if after.CanonicalTargetRevision != before.CanonicalTargetRevision ||
			after.Endpoint.CanReachManagement != hostresources.CapabilityNo {
			t.Fatalf("inactive redirect changed active target: before=%#v after=%#v", before, after)
		}
	})

	for _, test := range []struct {
		name, target, reason string
		external             bool
	}{
		{name: "external https", target: "https://panel.example.com/secret-management?q=private#fragment", external: true, reason: "external_redirect_unverified"},
		{name: "external http", target: "http://panel.example.com/secret-management?q=private", external: true, reason: "external_redirect_unverified"},
		{name: "scheme relative", target: "//panel.example.com/secret-management", external: true, reason: "redirect_target_invalid"},
		{name: "malformed", target: "https://user:password@panel.example.com/secret-management", external: true, reason: "redirect_target_invalid"},
	} {
		t.Run(test.name+" is non-actionable and redacted", func(t *testing.T) {
			db := openProviderDB(t, "redirect-"+strings.ReplaceAll(test.name, " ", "-"))
			site, _ := createProviderFixture(t, db, false, "", 18443)
			provider := targetProvider{db: db, now: func() time.Time { return fixed }}
			safe := providerTargetFromInventory(t, provider)
			reference, err := neutral.ReferenceV2FromTarget(safe)
			if err != nil {
				t.Fatalf("safe reference: %v", err)
			}
			publish := activeProviderPublish(t, db, site.ID)
			if err := db.Create(&fallbackdomain.PublishRedirect{
				PublishID: publish.ID, FromPath: "/go/", ToPath: test.target, StatusCode: 302, External: test.external,
			}).Error; err != nil {
				t.Fatalf("create unsafe redirect: %v", err)
			}
			inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
			if providerErr != nil || len(inventory.Targets) != 0 ||
				!strings.Contains(strings.Join(inventory.ReasonCodes, ","), test.reason) {
				t.Fatalf("unsafe redirect inventory = %#v, %v", inventory, providerErr)
			}
			encoded, _ := json.Marshal(inventory)
			for _, forbidden := range []string{"panel.example.com", "secret-management", "password", "q=private", "fragment"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("unsafe redirect leaked %q: %s", forbidden, encoded)
				}
			}
			status, statusErr := fallbackProviderStatus(context.Background(), db, nil, site.ID, fixed)
			statusJSON, _ := json.Marshal(status)
			if statusErr != nil || !strings.Contains(strings.Join(status.ReasonCodes, ","), test.reason) {
				t.Fatalf("unsafe redirect provider status = %#v, %v", status, statusErr)
			}
			for _, forbidden := range []string{"panel.example.com", "secret-management", "password", "q=private", "fragment"} {
				if strings.Contains(string(statusJSON), forbidden) {
					t.Fatalf("provider status leaked %q: %s", forbidden, statusJSON)
				}
			}
			if _, resolveErr := provider.ResolveV2(context.Background(), reference); resolveErr == nil ||
				resolveErr.ReasonCode != test.reason || strings.Contains(resolveErr.Error(), "panel.example.com") {
				t.Fatalf("unsafe redirect resolve error = %#v", resolveErr)
			}
			if _, reserveErr := provider.Reserve(context.Background(), neutral.ReserveRequestV1{
				RequestID: "redirect-reserve", HolderID: "redirect-holder", Purpose: neutral.ReservationPurposeNativeFallback,
				ExactTargetReference: reference, FreshnessDurationSecs: 60,
			}); reserveErr == nil {
				t.Fatal("unsafe redirect acquired a provider reservation")
			}
			var reservationCount int64
			if err := db.Model(&authority.ReservationModel{}).Count(&reservationCount).Error; err != nil || reservationCount != 0 {
				t.Fatalf("unsafe redirect reservation count = %d, %v", reservationCount, err)
			}
		})
	}
}

func TestTargetProviderV2UnsafeRedirectDoesNotReleaseExistingAuthority(t *testing.T) {
	fixed := time.Unix(1_800_000_075, 0).UTC()
	db := openProviderDB(t, "redirect-authority")
	site, _ := createProviderFixture(t, db, false, "", 18443)
	provider := targetProvider{db: db, now: func() time.Time { return fixed }}
	target := providerTargetFromInventory(t, provider)
	reference, err := neutral.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatalf("target reference: %v", err)
	}
	reservation := neutral.ProviderTargetReservationV1{
		Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: "redirect-guard",
		ReservationRevision: "redirect-guard-revision", HolderID: "redirect-holder",
		Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference,
		State: neutral.ReservationActive, IssuedAt: fixed.Unix(), RenewedAt: fixed.Unix(),
		FreshnessExpiresAt: fixed.Add(time.Minute).Unix(),
	}
	row, err := authority.EncodeReservation(reservation, fixed.Unix(), fixed.Unix())
	if err != nil {
		t.Fatalf("encode reservation: %v", err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create reservation: %v", err)
	}
	publish := activeProviderPublish(t, db, site.ID)
	if err := db.Create(&fallbackdomain.PublishRedirect{
		PublishID: publish.ID, FromPath: "/go/", ToPath: "https://panel.example.com/management", StatusCode: 302, External: true,
	}).Error; err != nil {
		t.Fatalf("create unsafe redirect: %v", err)
	}
	inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
	if providerErr != nil || len(inventory.Targets) != 0 {
		t.Fatalf("unsafe guarded target inventory = %#v, %v", inventory, providerErr)
	}
	var preserved authority.ReservationModel
	if err := db.Where("reservation_id = ?", reservation.ReservationID).First(&preserved).Error; err != nil {
		t.Fatalf("guarding reservation was removed: %v", err)
	}
	decoded, err := authority.DecodeReservation(preserved)
	if err != nil || decoded.State != neutral.ReservationActive || decoded.ReservationRevision != reservation.ReservationRevision {
		t.Fatalf("guarding reservation changed: %#v, %v", decoded, err)
	}
	if err := authority.GuardSiteMutation(db, id, target.Identity.TargetID, fixed); err == nil {
		t.Fatal("unsafe redirect released the existing provider guard")
	}
}

func TestTargetProviderV2ReservationLifecycleReplayAndGuards(t *testing.T) {
	db := openProviderDB(t, "lifecycle")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(runtime.Stop)
	now := time.Unix(1_800_000_100, 0).UTC()
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	target := onlyActionableTarget(t, provider)
	if neutral.EffectiveReadinessV2(target.Health, time.Unix(target.Health.ExpiresAt+1, 0)) != neutral.ReadinessStale {
		t.Fatalf("ready health did not become stale")
	}
	reference, err := neutral.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	resolved, resolveErr := provider.ResolveV2(context.Background(), reference)
	if resolveErr != nil || resolved.Target.CanonicalTargetRevision != target.CanonicalTargetRevision {
		t.Fatalf("ResolveV2 = %#v, %v", resolved, resolveErr)
	}
	reserveRequest := neutral.ReserveRequestV1{RequestID: "reserve-one", HolderID: "holder-one", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 120}
	reserved, providerErr := provider.Reserve(context.Background(), reserveRequest)
	if providerErr != nil || reserved.Reservation.State != neutral.ReservationReserved {
		t.Fatalf("Reserve = %#v, %v", reserved, providerErr)
	}
	replayed, providerErr := provider.Reserve(context.Background(), reserveRequest)
	if providerErr != nil || replayed.Reservation.ReservationRevision != reserved.Reservation.ReservationRevision {
		t.Fatalf("reserve replay = %#v, %v", replayed, providerErr)
	}
	conflictRequest := reserveRequest
	conflictRequest.HolderID = "holder-two"
	if _, providerErr = provider.Reserve(context.Background(), conflictRequest); providerErr == nil || providerErr.Class != neutral.ProviderErrorConflict {
		t.Fatalf("conflicting replay error = %#v", providerErr)
	}

	now = now.Add(time.Second)
	fenced, providerErr := provider.FenceForMutation(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "fence-one", ReservationID: reserved.Reservation.ReservationID, ExpectedRevision: reserved.Reservation.ReservationRevision})
	if providerErr != nil || fenced.Reservation.State != neutral.ReservationMutationPending {
		t.Fatalf("FenceForMutation = %#v, %v", fenced, providerErr)
	}
	if _, staleErr := provider.Activate(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "activate-stale", ReservationID: fenced.Reservation.ReservationID, ExpectedRevision: reserved.Reservation.ReservationRevision, FreshnessDurationSecs: 300}); staleErr == nil || staleErr.Class != neutral.ProviderErrorConflict {
		t.Fatalf("stale activate error = %#v", staleErr)
	}

	now = now.Add(time.Second)
	active, providerErr := provider.Activate(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "activate-one", ReservationID: fenced.Reservation.ReservationID, ExpectedRevision: fenced.Reservation.ReservationRevision, FreshnessDurationSecs: 300})
	if providerErr != nil || active.Reservation.State != neutral.ReservationActive {
		t.Fatalf("Activate = %#v, %v", active, providerErr)
	}
	if err := authority.GuardSiteMutation(db, id, target.Identity.TargetID, now); err == nil {
		t.Fatalf("active reservation did not guard target")
	}

	now = now.Add(time.Second)
	renewed, providerErr := provider.Renew(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "renew-one", ReservationID: active.Reservation.ReservationID, ExpectedRevision: active.Reservation.ReservationRevision, FreshnessDurationSecs: 400})
	if providerErr != nil || renewed.Reservation.RenewedAt <= active.Reservation.RenewedAt {
		t.Fatalf("Renew = %#v, %v", renewed, providerErr)
	}

	now = now.Add(time.Second)
	released, providerErr := provider.Release(context.Background(), neutral.ReleaseReservationRequestV1{RequestID: "release-one", ReservationID: renewed.Reservation.ReservationID, ExpectedRevision: renewed.Reservation.ReservationRevision, VerifiedDetachedRevision: strings.Repeat("a", 64)})
	if providerErr != nil || released.Reservation.State != neutral.ReservationReleased {
		t.Fatalf("Release = %#v, %v", released, providerErr)
	}
	if err := authority.GuardSiteMutation(db, id, target.Identity.TargetID, now); err != nil {
		t.Fatalf("released reservation still guarded target: %v", err)
	}
	if _, terminalErr := provider.Renew(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "renew-terminal", ReservationID: released.Reservation.ReservationID, ExpectedRevision: released.Reservation.ReservationRevision, FreshnessDurationSecs: 100}); terminalErr == nil || terminalErr.Class != neutral.ProviderErrorConflict {
		t.Fatalf("terminal renew error = %#v", terminalErr)
	}
	listed, providerErr := provider.ListReservations(context.Background(), neutral.ListReservationsQueryV1{ProviderID: id, Limit: 1})
	if providerErr != nil || len(listed.Reservations) != 1 || listed.Reservations[0].ReservationID != released.Reservation.ReservationID {
		t.Fatalf("ListReservations = %#v, %v", listed, providerErr)
	}
}

func TestTargetProviderV2OmitsManagementUnsafeModesAndReportsExhaustion(t *testing.T) {
	now := time.Unix(1_800_000_150, 0).UTC()
	t.Run("management unsafe mode", func(t *testing.T) {
		db := openProviderDB(t, "management-unsafe")
		_, target := createProviderFixture(t, db, false, "", 18080)
		if err := db.Model(&fallbackdomain.RuntimeTarget{}).Where("id = ?", target.ID).Update("kind", "web-current").Error; err != nil {
			t.Fatalf("change target kind: %v", err)
		}
		inventory, providerErr := (targetProvider{db: db, now: func() time.Time { return now }}).InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
		if providerErr != nil || len(inventory.Targets) != 0 || !strings.Contains(strings.Join(inventory.ReasonCodes, ","), "runtime_target_unsupported") {
			t.Fatalf("unsafe inventory = %#v, %v", inventory, providerErr)
		}
	})

	t.Run("exhausted authority", func(t *testing.T) {
		db := openProviderDB(t, "exhausted")
		createProviderFixture(t, db, false, "", freeProviderPort(t))
		runtime := fallbackservice.NewRuntime()
		if err := runtime.Start(db); err != nil {
			t.Fatalf("start runtime: %v", err)
		}
		t.Cleanup(runtime.Stop)
		provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
		target := onlyActionableTarget(t, provider)
		reference, _ := neutral.ReferenceV2FromTarget(target)
		for index := uint32(0); index < authority.SlotsPerTarget; index++ {
			reservation := neutral.ProviderTargetReservationV1{Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: "exhausted-" + providerTestUintString(uint(index)), ReservationRevision: "revision-" + providerTestUintString(uint(index)), HolderID: "holder-" + providerTestUintString(uint(index)), Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, State: neutral.ReservationReconcileRequired, IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix()}
			row, err := authority.EncodeReservation(reservation, now.Unix(), now.Unix())
			if err != nil {
				t.Fatalf("encode reservation: %v", err)
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create reservation: %v", err)
			}
		}
		inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
		if providerErr != nil || len(inventory.Targets) != 1 || inventory.Targets[0].Capacity.State != neutral.CapacityExhausted || inventory.Targets[0].Capacity.ReservationSlotsUsed != authority.SlotsPerTarget {
			t.Fatalf("exhausted inventory = %#v, %v", inventory, providerErr)
		}
	})
}

func TestTargetProviderV2CancellationDoesNotWriteReservation(t *testing.T) {
	db := openProviderDB(t, "cancel-reserve")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(runtime.Stop)
	now := time.Unix(1_800_000_175, 0).UTC()
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	target := onlyActionableTarget(t, provider)
	reference, _ := neutral.ReferenceV2FromTarget(target)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, providerErr := provider.Reserve(ctx, neutral.ReserveRequestV1{RequestID: "cancel-reserve", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60})
	if providerErr == nil {
		t.Fatalf("cancelled reserve succeeded")
	}
	var count int64
	if err := db.Model(&authority.ReservationModel{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("cancelled reserve rows = %d, %v", count, err)
	}
}

func TestTargetProviderV2StorageFailureRollsBackReservationAndRedactsError(t *testing.T) {
	db := openProviderDB(t, "rollback")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(runtime.Stop)
	now := time.Unix(1_800_000_180, 0).UTC()
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	target := onlyActionableTarget(t, provider)
	reference, _ := neutral.ReferenceV2FromTarget(target)
	if err := db.Exec("CREATE TRIGGER fail_reservation_replay BEFORE INSERT ON fallback_html_target_reservation_requests BEGIN SELECT RAISE(ABORT, 'injected storage path C:/secret.db'); END").Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	_, providerErr := provider.Reserve(context.Background(), neutral.ReserveRequestV1{RequestID: "rollback-reserve", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60})
	if providerErr == nil || providerErr.Class != neutral.ProviderErrorInternal || strings.Contains(providerErr.Error(), "secret") || strings.Contains(providerErr.Error(), "sqlite") {
		t.Fatalf("safe storage error = %#v", providerErr)
	}
	var count int64
	if err := db.Model(&authority.ReservationModel{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed transaction retained %d reservations: %v", count, err)
	}
}

func TestTargetProviderV2EnforcesGlobalAuthorityLimit(t *testing.T) {
	db := openProviderDB(t, "global-limit")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(runtime.Stop)
	now := time.Unix(1_800_000_185, 0).UTC()
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	target := onlyActionableTarget(t, provider)
	reference, _ := neutral.ReferenceV2FromTarget(target)
	rows := make([]authority.ReservationModel, 0, neutral.MaxReservationsV2)
	for index := 0; index < neutral.MaxReservationsV2; index++ {
		otherReference := reference
		otherReference.TargetID = "other:" + providerTestUintString(uint(index+1))
		reservation := neutral.ProviderTargetReservationV1{Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: "global-" + providerTestUintString(uint(index+1)), ReservationRevision: "revision-" + providerTestUintString(uint(index+1)), HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: otherReference, State: neutral.ReservationReconcileRequired, IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix()}
		row, err := authority.EncodeReservation(reservation, now.Unix(), now.Unix())
		if err != nil {
			t.Fatalf("encode global reservation %d: %v", index, err)
		}
		rows = append(rows, row)
	}
	if err := db.CreateInBatches(&rows, 256).Error; err != nil {
		t.Fatalf("insert global authority: %v", err)
	}
	_, providerErr := provider.Reserve(context.Background(), neutral.ReserveRequestV1{RequestID: "global-limit", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60})
	if providerErr == nil || providerErr.Class != neutral.ProviderErrorCapacity || providerErr.ReasonCode != "provider_capacity_exhausted" {
		t.Fatalf("global limit error = %#v", providerErr)
	}
}

func TestTargetProviderV2RejectsRenewOfExpiredActiveReservation(t *testing.T) {
	db := openProviderDB(t, "expired-renew")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(runtime.Stop)
	now := time.Unix(1_800_000_190, 0).UTC()
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	target := onlyActionableTarget(t, provider)
	reference, _ := neutral.ReferenceV2FromTarget(target)
	reserved, providerErr := provider.Reserve(context.Background(), neutral.ReserveRequestV1{RequestID: "expired-reserve", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60})
	if providerErr != nil {
		t.Fatalf("reserve: %v", providerErr)
	}
	now = now.Add(time.Second)
	fenced, providerErr := provider.FenceForMutation(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "expired-fence", ReservationID: reserved.Reservation.ReservationID, ExpectedRevision: reserved.Reservation.ReservationRevision})
	if providerErr != nil {
		t.Fatalf("fence: %v", providerErr)
	}
	active, providerErr := provider.Activate(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "expired-activate", ReservationID: fenced.Reservation.ReservationID, ExpectedRevision: fenced.Reservation.ReservationRevision, FreshnessDurationSecs: 61})
	if providerErr != nil {
		t.Fatalf("activate: %v", providerErr)
	}
	now = now.Add(62 * time.Second)
	if _, renewErr := provider.Renew(context.Background(), neutral.ReservationMutationRequestV1{RequestID: "expired-renew", ReservationID: active.Reservation.ReservationID, ExpectedRevision: active.Reservation.ReservationRevision, FreshnessDurationSecs: 60}); renewErr == nil || renewErr.Class != neutral.ProviderErrorConflict {
		t.Fatalf("expired renew error = %#v", renewErr)
	}
	if err := authority.GuardSiteMutation(db, id, target.Identity.TargetID, now); err == nil {
		t.Fatalf("expired ACTIVE stopped guarding target")
	}
}

func TestTargetProviderV2ConcurrentFinalActionableSlotDoesNotOversubscribe(t *testing.T) {
	db := openProviderDB(t, "concurrency")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(runtime.Stop)
	now := time.Unix(1_800_000_200, 0).UTC()
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	for index := 0; index < 2; index++ {
		target := onlyActionableTarget(t, provider)
		reference, _ := neutral.ReferenceV2FromTarget(target)
		request := neutral.ReserveRequestV1{RequestID: "prefill-" + providerTestUintString(uint(index+1)), HolderID: "holder-" + providerTestUintString(uint(index+1)), Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 120}
		if _, providerErr := provider.Reserve(context.Background(), request); providerErr != nil {
			t.Fatalf("prefill reserve %d: %v", index, providerErr)
		}
	}
	target := onlyActionableTarget(t, provider)
	reference, _ := neutral.ReferenceV2FromTarget(target)
	var wg sync.WaitGroup
	results := make(chan *neutral.ProviderContractError, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, providerErr := provider.Reserve(context.Background(), neutral.ReserveRequestV1{RequestID: "race-" + providerTestUintString(uint(index+1)), HolderID: "race-holder-" + providerTestUintString(uint(index+1)), Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 120})
			results <- providerErr
		}(index)
	}
	wg.Wait()
	close(results)
	successes := 0
	for providerErr := range results {
		if providerErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent reserve successes = %d, want 1", successes)
	}
	used, err := authority.CountGuardingTarget(db, id, target.Identity.TargetID, now)
	if err != nil || used != providerPressureSlots {
		t.Fatalf("guarding reservations = %d, %v", used, err)
	}
	inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
	if providerErr != nil || len(inventory.Targets) != 1 || inventory.Targets[0].Capacity.State != neutral.CapacityPressured {
		t.Fatalf("pressured inventory = %#v, %v", inventory, providerErr)
	}
	firstPage, providerErr := provider.ListReservations(context.Background(), neutral.ListReservationsQueryV1{ProviderID: id, Limit: 2})
	if providerErr != nil || !firstPage.Truncated || len(firstPage.Reservations) != 2 || firstPage.Continuation == "" {
		t.Fatalf("first reservation page = %#v, %v", firstPage, providerErr)
	}
	secondPage, providerErr := provider.ListReservations(context.Background(), neutral.ListReservationsQueryV1{ProviderID: id, Limit: 2, Continuation: firstPage.Continuation})
	if providerErr != nil || secondPage.Truncated || len(secondPage.Reservations) != 1 || secondPage.Reservations[0].ReservationID <= firstPage.Continuation {
		t.Fatalf("second reservation page = %#v, %v", secondPage, providerErr)
	}
}

func openProviderDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dsn := "file:fallback-provider-" + name + "?mode=memory&cache=shared&_busy_timeout=10000&_foreign_keys=on"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite pool: %v", err)
	}
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	if err := fallbackdomain.EnsureSchema(db); err != nil {
		t.Fatalf("domain schema: %v", err)
	}
	if err := authority.EnsureSchema(db); err != nil {
		t.Fatalf("authority schema: %v", err)
	}
	return db
}

func createProviderFixture(t *testing.T, db *gorm.DB, tls bool, host string, port int) (fallbackdomain.Site, fallbackdomain.RuntimeTarget) {
	t.Helper()
	site := fallbackdomain.Site{Name: "Provider fixture", Enabled: true, Status: "published", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	target := fallbackdomain.RuntimeTarget{SiteID: site.ID, Kind: "standalone", Host: host, Listen: "127.0.0.1", Port: port, RootPath: "/", Runtime: "gin", TLS: tls, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target: %v", err)
	}
	root := t.TempDir()
	publish := fallbackdomain.Publish{SiteID: site.ID, Version: "publish-1", RootDir: root, Active: true, CreatedAt: 1}
	if err := db.Create(&publish).Error; err != nil {
		t.Fatalf("create publish: %v", err)
	}
	for index, item := range []struct{ path, body string }{{"/", "<html>home</html>"}, {"/404.html", "<html>missing</html>"}} {
		filename := "page-" + providerTestUintString(uint(index)) + ".html"
		filePath := filepath.Join(root, filename)
		if err := os.WriteFile(filePath, []byte(item.body), 0o600); err != nil {
			t.Fatalf("write publish file: %v", err)
		}
		sum := sha256.Sum256([]byte(item.body))
		row := fallbackdomain.PublishFile{PublishID: publish.ID, PublicPath: item.path, FilePath: filePath, MimeType: "text/html; charset=utf-8", Sha256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(item.body))}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create publish file: %v", err)
		}
	}
	return site, target
}

func freeProviderPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func onlyActionableTarget(t *testing.T, provider targetProvider) neutral.FallbackTargetV2 {
	t.Helper()
	inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
	if providerErr != nil || len(inventory.Targets) != 1 {
		t.Fatalf("InventoryV2 = %#v, %v", inventory, providerErr)
	}
	target := inventory.Targets[0]
	if target.Health.Readiness != neutral.ReadinessReady || target.Capacity.State != neutral.CapacityReady {
		t.Fatalf("target is not actionable: %#v", target)
	}
	return target
}

func providerTargetFromInventory(t *testing.T, provider targetProvider) neutral.FallbackTargetV2 {
	t.Helper()
	inventory, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8})
	if providerErr != nil || len(inventory.Targets) != 1 {
		t.Fatalf("InventoryV2 = %#v, %v", inventory, providerErr)
	}
	return inventory.Targets[0]
}

func activeProviderPublish(t *testing.T, db *gorm.DB, siteID uint) fallbackdomain.Publish {
	t.Helper()
	var publish fallbackdomain.Publish
	if err := db.Where("site_id = ? AND active = ?", siteID, true).First(&publish).Error; err != nil {
		t.Fatalf("active publish: %v", err)
	}
	return publish
}

func equalProtocols(left, right []neutral.ApplicationProtocol) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func providerTestUintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
