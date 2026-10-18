package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func updateTestPlatformFacts(now time.Time) domain.PlatformFacts {
	facts := domain.UnknownPlatformFacts("not_observed", now)
	facts.Source, facts.EvidenceRevision = domain.ProviderV1, domain.Revision("posture")
	facts.ObservedAt, facts.ExpiresAt = now.Unix(), now.Add(time.Minute).Unix()
	facts.ServiceSupervision = domain.PlatformFact{Availability: domain.Available, Backend: "systemd", Reason: "observed"}
	facts.PrivilegeExecution = domain.PlatformFact{Availability: domain.Available, Backend: "closed-broker", Reason: "observed"}
	facts.UpdateLifecycle = domain.PlatformFact{Availability: domain.Available, Backend: "self-managed", Reason: "observed"}
	facts.Revision = ""
	facts.Revision = domain.Revision(facts)
	return facts
}

func TestUpdateFreshPlatformPrerequisitesGateStagingAndActivation(t *testing.T) {
	fixture := newLifecycleFixture(t)
	fixture.manager.platformFacts = func(context.Context) domain.PlatformFacts {
		return domain.UnknownPlatformFacts("unavailable", fixture.manager.now())
	}
	_, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("platform-unknown"))
	var denied PlatformPreflightError
	if !errors.As(err, &denied) || denied.Report.Ready || ReasonCode(err) != "update_platform_preflight_failed" {
		t.Fatalf("unknown admitted: %v", err)
	}
	var count int64
	if err := fixture.db.Model(&model.UpdateOperation{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preflight created durable mutation: %d %v", count, err)
	}
	fixture.manager.platformFacts = func(context.Context) domain.PlatformFacts { return updateTestPlatformFacts(fixture.manager.now()) }
	prepared, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("platform-current"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.manager.platformFacts = func(context.Context) domain.PlatformFacts {
		return domain.UnknownPlatformFacts("unavailable", fixture.manager.now())
	}
	result, err := fixture.manager.Activate(t.Context(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
	if !errors.As(err, &denied) || result.State != prepared.State || result.Revision != prepared.Revision {
		t.Fatalf("activation bypassed fresh prerequisites: %+v %v", result, err)
	}
}

func TestCancelDuringPlatformPreflightCannotAdmitUpdate(t *testing.T) {
	fixture := newLifecycleFixture(t)
	entered := make(chan struct{})
	fixture.manager.platformFacts = func(ctx context.Context) domain.PlatformFacts {
		close(entered)
		<-ctx.Done()
		return updateTestPlatformFacts(fixture.manager.now())
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := fixture.manager.Prepare(ctx, fixture.prepareRequest("platform-cancel")); done <- err }()
	<-entered
	cancel()
	var denied PlatformPreflightError
	if err := <-done; !errors.As(err, &denied) {
		t.Fatalf("cancelled preflight result=%v", err)
	}
	var count int64
	if err := fixture.db.Model(&model.UpdateOperation{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("cancelled preflight admitted=%d err=%v", count, err)
	}
}
