//go:build !minimal

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/gorm"
)

func TestProviderReservationGuardsEveryTargetInvalidatingServiceMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Service, fallbackdomain.Site) error
	}{
		{name: "delete site", mutate: func(service *Service, site fallbackdomain.Site) error { return service.DeleteSite(site.ID, "tester") }},
		{name: "disable site", mutate: func(service *Service, site fallbackdomain.Site) error {
			disabled := false
			_, err := service.SaveSite(SiteInput{ID: site.ID, Name: site.Name, TemplateID: site.TemplateID, Enabled: &disabled}, "tester")
			return err
		}},
		{name: "change listener", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.SaveTarget(site.ID, TargetInput{ID: site.Targets[0].ID, Kind: "standalone", Listen: "127.0.0.1", Port: 18081, Runtime: "gin"}, "tester")
			return err
		}},
		{name: "change tls mode", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.SaveTarget(site.ID, TargetInput{ID: site.Targets[0].ID, Kind: "standalone", Host: "decoy.example", Listen: "127.0.0.1", Port: 18082, Runtime: "gin", TLS: true}, "tester")
			return err
		}},
		{name: "change server name", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.SaveTarget(site.ID, TargetInput{ID: site.Targets[0].ID, Kind: "standalone", Host: "other.example", Listen: "127.0.0.1", Port: 18083, Runtime: "gin"}, "tester")
			return err
		}},
		{name: "delete target", mutate: func(service *Service, site fallbackdomain.Site) error {
			return service.DeleteTarget(site.ID, site.Targets[0].ID, "tester")
		}},
		{name: "replace publish", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.PublishSite(site.ID, "tester")
			return err
		}},
		{name: "unpublish", mutate: func(service *Service, site fallbackdomain.Site) error {
			return service.UnpublishSite(site.ID, "tester")
		}},
		{name: "rollback", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.RollbackSite(site.ID, RollbackInput{}, "tester")
			return err
		}},
		{name: "prune publish files", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.PrunePublishes(site.ID, PrunePublishesInput{Keep: 0}, "tester")
			return err
		}},
		{name: "replace imported content", mutate: func(service *Service, site fallbackdomain.Site) error {
			_, err := service.ImportSite(site.ID, SiteImportInput{Schema: "solovey-ui/fallback-html-import/v1", Pages: []SiteImportPage{{Path: "/", Title: "Imported", Body: "Imported"}}}, "tester")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, db, site := guardedServiceFixture(t)
			insertGuardReservation(t, db, site.ID, neutral.ReservationActive, time.Now().UTC())
			if err := test.mutate(service, site); err == nil {
				t.Fatalf("mutation was not blocked")
			}
			var count int64
			if err := db.Model(&fallbackdomain.Site{}).Where("id = ?", site.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("blocked mutation changed site: count=%d err=%v", count, err)
			}
		})
	}
}

func TestProviderReservationAllowsDraftAndUnrelatedMutationsAndReleasedAuthority(t *testing.T) {
	service, db, site := guardedServiceFixture(t)
	insertGuardReservation(t, db, site.ID, neutral.ReservationActive, time.Now().UTC())
	if _, err := service.SavePage(site.ID, PageInput{Path: "/draft", Title: "Draft", Body: "not published"}, "tester"); err != nil {
		t.Fatalf("edit unpublished draft: %v", err)
	}
	unrelated, err := service.SaveSite(SiteInput{Name: "Unrelated"}, "tester")
	if err != nil {
		t.Fatalf("create unrelated site: %v", err)
	}
	if _, err := service.SaveTarget(unrelated.ID, TargetInput{Kind: "standalone", Listen: "127.0.0.1", Port: 18091, Runtime: "gin"}, "tester"); err != nil {
		t.Fatalf("create unrelated target: %v", err)
	}
	if err := db.Model(&authority.ReservationModel{}).Where("target_id = ?", "site:"+uintString(site.ID)).Updates(map[string]any{"state": string(neutral.ReservationReleased), "released_at": time.Now().Unix()}).Error; err != nil {
		t.Fatalf("release fixture: %v", err)
	}
	if err := service.UnpublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("released authority did not allow mutation: %v", err)
	}
}

func TestRestorePostOpenMovesActiveAuthorityToReconcileRequiredIdempotently(t *testing.T) {
	_, db, site := guardedServiceFixture(t)
	now := time.Now().UTC()
	insertGuardReservation(t, db, site.ID, neutral.ReservationActive, now)
	if err := HandleRestorePostOpen(context.Background(), db); err != nil {
		t.Fatalf("HandleRestorePostOpen: %v", err)
	}
	if err := HandleRestorePostOpen(context.Background(), db); err != nil {
		t.Fatalf("idempotent HandleRestorePostOpen: %v", err)
	}
	var row authority.ReservationModel
	if err := db.First(&row, "target_id = ?", "site:"+uintString(site.ID)).Error; err != nil {
		t.Fatalf("load restored reservation: %v", err)
	}
	reservation, err := authority.DecodeReservation(row)
	if err != nil || reservation.State != neutral.ReservationReconcileRequired || !reservation.Status(time.Now().UTC()).BlocksMutation {
		t.Fatalf("restored reservation = %#v, %v", reservation, err)
	}
	DefaultRuntime.Stop()
}

func guardedServiceFixture(t *testing.T) (*Service, *gorm.DB, fallbackdomain.Site) {
	t.Helper()
	db, _ := openFallbackDB(t)
	if err := authority.EnsureSchema(db); err != nil {
		t.Fatalf("authority schema: %v", err)
	}
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Guarded"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if _, err := service.SavePage(site.ID, PageInput{Path: "/second", Title: "Second", Body: "Second"}, "tester"); err != nil {
		t.Fatalf("save second page: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	site, err = service.GetSite(site.ID)
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	return service, db, site
}

func insertGuardReservation(t *testing.T, db *gorm.DB, siteID uint, state neutral.ReservationState, now time.Time) {
	t.Helper()
	target, err := neutral.FinalizeFallbackTargetV2(neutral.FallbackTargetV2{
		Identity:         neutral.TargetIdentity{ProviderID: reservationProviderID, TargetID: "site:" + uintString(siteID)},
		Publish:          neutral.PublishFactsV2{Revision: "publish-guard", ContentDigest: strings.Repeat("a", 64)},
		Endpoint:         neutral.EndpointV2{EndpointID: "endpoint-guard", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, Address: "127.0.0.1", Port: 8080, Local: true, TransportSecurity: neutral.TransportSecurityPlaintext, ApplicationProtocols: []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11}, ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: hostresources.CapabilityNo},
		Health:           neutral.HealthV2{Readiness: neutral.ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Capacity:         neutral.CapacityV2{State: neutral.CapacityReady, ReservationSlotsTotal: authority.SlotsPerTarget, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "provider-guard", Source: "fallback-html-runtime", ConfidenceBP: 10000,
	})
	if err != nil {
		t.Fatalf("finalize target: %v", err)
	}
	reference, _ := neutral.ReferenceV2FromTarget(target)
	reservation := neutral.ProviderTargetReservationV1{Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: "guard-" + uintString(siteID), ReservationRevision: "guard-revision", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, State: state, IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix()}
	row, err := authority.EncodeReservation(reservation, now.Unix(), now.Unix())
	if err != nil {
		t.Fatalf("encode reservation: %v", err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create reservation: %v", err)
	}
}
