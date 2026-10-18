package update

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func TestSignedUpdateJobReconnectKeepsDurableProgressAndOutcome(t *testing.T) {
	fixture := newLifecycleFixture(t)
	prepared, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("job-reconnect"))
	if err != nil {
		t.Fatal(err)
	}
	job := ProjectJob(prepared)
	if job.JobID == "" || job.Progress == nil || job.Progress.Completed != job.Progress.Total || job.Progress.Total <= 0 ||
		job.Outcome != operationcoordination.OutcomePending || job.CriticalSection || job.CancelSupported || job.CancelRequested != nil {
		t.Fatalf("prepared job is not truthful: %+v", job)
	}
	status := fixture.manager.Status(t.Context(), release.ChannelMain)
	if status.Job == nil || !reflect.DeepEqual(*status.Job, job) {
		t.Fatalf("reconnect differs from preparation: %+v %+v", status.Job, job)
	}
	late := prepared
	late.State, late.Revision, late.BytesCompleted = string(StateDownloading), 1, 0
	if err := fixture.manager.repo.progress(t.Context(), late, 1); !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("late progress was not fenced: %v", err)
	}
	if err := fixture.manager.repo.progress(t.Context(), late, -1); err == nil {
		t.Fatal("negative progress accepted")
	}
	activated, err := fixture.manager.Activate(t.Context(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
	if err != nil {
		t.Fatal(err)
	}
	terminal := ProjectJob(activated)
	if terminal.JobID != job.JobID || terminal.Outcome != operationcoordination.OutcomeCommitted || terminal.ResultRef != activated.OperationID || terminal.CriticalSection {
		t.Fatalf("terminal job lost owner truth: %+v", terminal)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.manager.Operation(ctx, activated.OperationID); err == nil {
		t.Fatal("cancelled read ignored context")
	}
	reconnected, err := fixture.manager.Operation(t.Context(), activated.OperationID)
	if err != nil || !reflect.DeepEqual(ProjectJob(reconnected), terminal) {
		t.Fatalf("request cancellation changed durable result: %+v %v", reconnected, err)
	}
	journal, err := fixture.manager.ReadJournal(t.Context(), 0, 201)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, event := range journal {
		if event.Type == "update_applied" && event.OperationID == activated.OperationID {
			matched = event.JobID == terminal.JobID && event.ChangeID == terminal.ChangeID && event.CorrelationID == terminal.CorrelationID
		}
	}
	if !matched {
		t.Fatal("signed Update acceptance is absent from correlated owner journal")
	}
}
