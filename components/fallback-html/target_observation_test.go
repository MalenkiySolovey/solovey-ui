//go:build !minimal

package fallbackhtml

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
)

func TestTargetObservationRejectsForeignStaleAndMissingAuthority(t *testing.T) {
	db := openProviderDB(t, "observation-scope")
	site, target := createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	observer := providerObserver(t, runtime)
	request := fallbackservice.TargetObservationRequest{Resource: fallbackservice.ConfiguredTargetIdentity(site.ID, target),
		PublicationRevision: "publish-1", ExpectedGeneration: observer.Generation()}
	result, err := observer.ObserveTarget(t.Context(), request)
	if err != nil || result.Availability != hostresources.CapabilityYes || result.Health.Evidence == nil {
		t.Fatalf("actual observation: %+v %v", result, err)
	}
	if request.Resource.ObservationStatus != hostresources.ResourceObservationUnknown || request.Resource.ObservedRevision != "" {
		t.Fatal("configured fact was promoted into observation")
	}
	for _, test := range []struct {
		name   string
		change func(*fallbackservice.TargetObservationRequest)
	}{
		{"foreign host", func(r *fallbackservice.TargetObservationRequest) {
			r.Resource.Host = hostref.Ref{Kind: "NODE", ID: "foreign"}
		}},
		{"missing host", func(r *fallbackservice.TargetObservationRequest) { r.Resource.Host = hostref.Ref{} }},
		{"foreign owner", func(r *fallbackservice.TargetObservationRequest) { r.Resource.Owner = "core" }},
		{"foreign resource", func(r *fallbackservice.TargetObservationRequest) { r.Resource.ResourceID = "core:panel:web" }},
		{"missing target", func(r *fallbackservice.TargetObservationRequest) {
			r.Resource.ResourceID = "component:fallback-html:site:999"
		}},
		{"stale config", func(r *fallbackservice.TargetObservationRequest) {
			r.Resource.DesiredRevision = hostresources.Revision("old")
		}},
		{"stale publish", func(r *fallbackservice.TargetObservationRequest) { r.PublicationRevision = "old" }},
		{"stale generation", func(r *fallbackservice.TargetObservationRequest) { r.ExpectedGeneration++ }},
		{"absent generation", func(r *fallbackservice.TargetObservationRequest) { r.ExpectedGeneration = 0 }},
		{"invented observation", func(r *fallbackservice.TargetObservationRequest) {
			r.Resource.ObservedRevision = r.Resource.DesiredRevision
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stale := request
			test.change(&stale)
			got, err := observer.ObserveTarget(t.Context(), stale)
			if err == nil || got.Availability != hostresources.CapabilityUnknown {
				t.Fatalf("unproven authority accepted: %+v %v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := observer.ObserveTarget(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := runtime.Rebuild(db); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.ObserveTarget(t.Context(), request); err == nil {
		t.Fatal("prior generation remained current after rebuild")
	}
	var rows int64
	if err := db.Model(&fallbackdomain.RuntimeTarget{}).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatal("read observation changed durable targets")
	}
}

type recordedTargetHealth struct{ result componenthealth.Result }

func (c recordedTargetHealth) ResourceID() string                           { return c.result.ResourceID }
func (c recordedTargetHealth) Check(context.Context) componenthealth.Result { return c.result }

func TestTargetObservationRequiresCanonicalFreshExactHealth(t *testing.T) {
	db := openProviderDB(t, "observation-health")
	site, target := createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	state := runtime.PublicationState()
	request := fallbackservice.TargetObservationRequest{Resource: fallbackservice.ConfiguredTargetIdentity(site.ID, target), PublicationRevision: "publish-1", ExpectedGeneration: state.Generation}
	for _, test := range []struct {
		name   string
		change func(*componenthealth.Result)
	}{
		{"stale", func(r *componenthealth.Result) { r.Evidence.ObservedAt = time.Now().Add(-time.Minute).Unix() }},
		{"future", func(r *componenthealth.Result) { r.Evidence.ObservedAt = time.Now().Add(time.Minute).Unix() }},
		{"foreign host", func(r *componenthealth.Result) { r.Evidence.HostRef = hostref.Ref{} }},
		{"foreign owner", func(r *componenthealth.Result) { r.Evidence.Owner = "core" }},
		{"foreign resource", func(r *componenthealth.Result) { r.Evidence.ResourceID = "core:panel:web" }},
		{"old generation", func(r *componenthealth.Result) { r.Evidence.ObservedRevision = hostresources.Revision("old") }},
		{"wrong proof", func(r *componenthealth.Result) { r.Evidence.ProofKind = componenthealth.ConfigValid }},
		{"missing", func(r *componenthealth.Result) { r.Evidence = nil }},
		{"unknown", func(r *componenthealth.Result) { r.Status = componenthealth.StatusMissingCapability }},
	} {
		t.Run(test.name, func(t *testing.T) {
			health := componenthealth.NewRegistry()
			result := componenthealth.WithEvidence(componenthealth.Result{ResourceID: request.Resource.ResourceID, Status: componenthealth.StatusOK, Check: "fallback_runtime", FactCode: "runtime_routes_ready"}, id, componenthealth.RuntimeReady, "fallback-runtime", state.EvidenceRevision(), time.Now(), 30*time.Second)
			test.change(&result)
			stop, err := health.Register(recordedTargetHealth{result})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			observer := targetResourceObserver{runtime: runtime, health: health}
			got, err := observer.ObserveTarget(t.Context(), request)
			if err != nil || got.Availability != hostresources.CapabilityUnknown {
				t.Fatalf("bad health became available: %+v %v", got, err)
			}
		})
	}
	// The actual dynamic checker must not attach this site's proof to another ID.
	observer := providerObserver(t, runtime)
	foreign := observer.health.Check(t.Context(), "component:fallback-html:site:999")
	if foreign.Evidence != nil {
		t.Fatal("dynamic checker proved a foreign site")
	}
	before := runtime.PublicationState()
	if !reflect.DeepEqual(before, state) {
		t.Fatal("read-only health changed runtime")
	}
}

func TestUnknownTargetHealthCannotLeakAReservation(t *testing.T) {
	db := openProviderDB(t, "unknown-reservation")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	provider := targetProvider{db: db}
	target := providerTargetFromInventory(t, provider)
	reference, err := neutral.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	_, providerErr := provider.Reserve(t.Context(), neutral.ReserveRequestV1{RequestID: "unknown-reserve", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60})
	if providerErr == nil {
		t.Fatal("unknown runtime was admitted")
	}
	var count int64
	if err := db.Table("fallback_html_target_reservations").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed admission leaked authority: %d %v", count, err)
	}
}

func TestReservationHealthNeedsNoAdditionalDatabaseConnection(t *testing.T) {
	db := openProviderDB(t, "single-connection-observation")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	provider := targetProvider{db: db, runtime: runtime, observer: providerObserver(t, runtime)}
	target := onlyActionableTarget(t, provider)
	reference, err := neutral.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := provider.Reserve(ctx, neutral.ReserveRequestV1{RequestID: "single-connection", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60}); err != nil {
		t.Fatal(err)
	}
}

func TestReservationReleaseRejectsUnknownAndStaleCleanup(t *testing.T) {
	db := openProviderDB(t, "release-authority")
	createProviderFixture(t, db, false, "", freeProviderPort(t))
	runtime := fallbackservice.NewRuntime()
	if err := runtime.Start(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	provider := targetProvider{db: db, runtime: runtime, observer: providerObserver(t, runtime)}
	target := onlyActionableTarget(t, provider)
	reference, err := neutral.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	reserved, providerErr := provider.Reserve(t.Context(), neutral.ReserveRequestV1{RequestID: "prepare-cleanup", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: 60})
	if providerErr != nil {
		t.Fatal(providerErr)
	}
	for _, request := range []neutral.ReleaseReservationRequestV1{
		{RequestID: "unknown-detach", ReservationID: reserved.Reservation.ReservationID, ExpectedRevision: reserved.Reservation.ReservationRevision},
		{RequestID: "stale-cleanup", ReservationID: reserved.Reservation.ReservationID, ExpectedRevision: "stale", VerifiedDetachedRevision: hostresources.Revision("detached")},
		{RequestID: "foreign-cleanup", ReservationID: "foreign", ExpectedRevision: reserved.Reservation.ReservationRevision, VerifiedDetachedRevision: hostresources.Revision("detached")},
	} {
		if _, err := provider.Release(t.Context(), request); err == nil {
			t.Fatal("unknown cleanup returned success")
		}
	}
	current, providerErr := provider.GetReservation(t.Context(), neutral.GetReservationRequestV1{ReservationID: reserved.Reservation.ReservationID})
	if providerErr != nil || !reflect.DeepEqual(current, reserved) {
		t.Fatal("rejected cleanup changed authority")
	}
	if err := authority.GuardSiteMutation(db, id, target.Identity.TargetID, time.Now()); err == nil {
		t.Fatal("unverified cleanup released the mutation guard")
	}
}
