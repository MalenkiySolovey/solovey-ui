package observation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWorkerStopDeadlineIsRetryableWithoutBlockingPastDeadline(t *testing.T) {
	worker := NewWorker()
	release := make(chan struct{})
	done := make(chan struct{})
	worker.running.Store(true)
	worker.done = done
	worker.cancel = func() {}
	go func() {
		<-release
		worker.running.Store(false)
		close(done)
	}()

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Stop(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("timed out Stop error = %v", err)
	}
	worker.mu.Lock()
	retained := worker.done == done && worker.stopping
	worker.mu.Unlock()
	if !retained {
		t.Fatal("timed out Stop forgot the still-running worker")
	}
	if err := worker.Start(protectionrepository.New(nil)); !errors.Is(err, ErrWorkerStopping) {
		t.Fatalf("Start while Stop is incomplete = %v", err)
	}
	close(release)
	if err := worker.Stop(context.Background()); err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.done != nil || worker.stopping || worker.repository != nil || worker.sub != nil {
		t.Fatal("completed retry did not clear worker ownership")
	}
}

type workerResourceContributor struct{}

func (workerResourceContributor) Owner() string { return "fixture" }
func (workerResourceContributor) ListProtectableResources(context.Context) ([]hostresources.ProtectableResource, error) {
	return []hostresources.ProtectableResource{{
		ID: "fixture:public-site", Kind: "public_site", Owner: "fixture", Name: "Fixture",
		Protocol: "http", Listen: "127.0.0.1", Port: 8080, Source: "fixture",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true},
	}}, nil
}

func TestWorkerFinalFlushPersistsSanitizedEventsAndStopsIntake(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "worker.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := protectionrepository.New(db)
	settings, _, err := repository.LoadSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	settings.ObservationBufferSize = 8
	settings.ObservationFlushIntervalMS = 100
	if err := repository.SaveSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	unregisterResource, err := hostresources.Register(workerResourceContributor{})
	if err != nil {
		t.Fatal(err)
	}
	defer unregisterResource()
	resource := hostresources.Refresh(context.Background()).Resources[0]
	now := time.Now().Unix()
	profile := protectionrepository.ProfileModel{
		ResourceID: resource.ID, ResourceKind: resource.Kind, ResourceOwner: resource.Owner,
		Enabled: true, Status: "active", Mode: string(domain.ProfileModeRecordOnly),
		ResourceFingerprint: resource.Fingerprint, AcceptedFingerprint: resource.Fingerprint, LastSeenFingerprint: resource.Fingerprint,
		ScoreThreshold: 5, GraylistTTLSeconds: 3600, DefaultAction: string(domain.DecisionRecordOnly),
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateProfile(context.Background(), &profile); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&protectionrepository.IPAllowlistModel{IPCIDR: "198.51.100.9/32", Reason: "test allowlist", CreatedBy: "tester", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorker()
	if err := worker.Start(repository); err != nil {
		t.Fatal(err)
	}
	publicsurface.EmitObservation(publicsurface.Observation{
		ResourceID: resource.ID, ResourceKind: resource.Kind, SourceIP: "198.51.100.9",
		MethodClass: "get", PathClass: "scanner_path", StatusClass: "4xx", UserAgentClass: "ua_scanner",
		BytesClass: "small", DurationClass: "fast", ObservedAt: time.Now().Unix(),
	})
	publicsurface.EmitObservation(publicsurface.Observation{
		ResourceID: resource.ID, ResourceKind: resource.Kind, ComponentID: "fixture-producer", SourceIP: "203.0.113.9",
		MethodClass: "get", PathClass: "scanner_path", StatusClass: "4xx", UserAgentClass: "ua_scanner",
		BytesClass: "small", DurationClass: "fast", ObservedAt: time.Now().Unix(),
	})
	stopContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.Stop(stopContext); err != nil {
		t.Fatal(err)
	}
	items, total, err := repository.ListEvents(context.Background(), protectionrepository.EventFilter{PageQuery: protectionrepository.PageQuery{Page: 1, Limit: 10}})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("events = %d/%d err=%v", len(items), total, err)
	}
	graylist, totalGray, err := repository.ListGraylist(context.Background(), protectionrepository.GraylistFilter{PageQuery: protectionrepository.PageQuery{Page: 1, Limit: 10}})
	if err != nil || totalGray != 1 || len(graylist) != 1 || graylist[0].Score != 5 || graylist[0].IPCIDR != "203.0.113.9/32" {
		t.Fatalf("graylist = %#v total=%d err=%v", graylist, totalGray, err)
	}
	status := worker.Status()
	if status.Running || status.Persisted != 2 || status.Allowlisted != 1 || status.DroppedBatches != 0 {
		t.Fatalf("worker status = %#v", status)
	}
	publicsurface.EmitObservation(publicsurface.Observation{ResourceID: resource.ID, ResourceKind: resource.Kind, PathClass: "scanner_path"})
	_, totalAfter, err := repository.ListEvents(context.Background(), protectionrepository.EventFilter{PageQuery: protectionrepository.PageQuery{Page: 1, Limit: 10}})
	if err != nil || totalAfter != total {
		t.Fatalf("events changed after stop: before=%d after=%d err=%v", total, totalAfter, err)
	}
}
