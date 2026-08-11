package fronting

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	helperinvoker "github.com/MalenkiySolovey/solovey-ui/components/server-protection/internal/normalci/helperinvoker"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type frontingAudit struct{}

func (frontingAudit) RecordHelperAudit(context.Context, protectionhelper.AuditEvent) error {
	return nil
}

type bundleRecorder struct{ count int }

func (r *bundleRecorder) CreateBundle(context.Context, protectionrepository.OperationLockModel, string) error {
	r.count++
	return nil
}

type frontingFixture struct {
	workflow   *Workflow
	nginx      *helperinvoker.Invoker
	bundles    *bundleRecorder
	repository *protectionrepository.Repository
	manager    *protectionoperations.Manager
	storage    *protectionartifacts.Storage
}

func TestManualSyncApplyIsDeterministicFencedAndIdempotent(t *testing.T) {
	workflow, nginx, _, _ := newFrontingWorkflow(t, passingFrontingHealth)
	preview := frontingPreviewInput()
	desired, err := GeneratePreview(preview)
	if err != nil {
		t.Fatal(err)
	}
	syncInput := SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: "fronting-happy", Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision}
	synced, err := workflow.Sync(context.Background(), syncInput)
	if err != nil || synced.State != protectionoperations.StatePrepared || synced.Validation != "passed" || synced.CandidateSHA256 != desired.GeneratedSHA256 {
		t.Fatalf("sync=%#v err=%v", synced, err)
	}
	applied, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied || applied.ActiveRevision != desired.DesiredRevision || nginx.Reloads != 1 {
		t.Fatalf("apply=%#v reloads=%d err=%v", applied, nginx.Reloads, err)
	}
	replayed, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
	if err != nil || replayed.OperationID != applied.OperationID || nginx.Reloads != 1 {
		t.Fatalf("idempotent apply=%#v reloads=%d err=%v", replayed, nginx.Reloads, err)
	}
	joined, err := workflow.Sync(context.Background(), syncInput)
	if err != nil || joined.OperationID != synced.OperationID || nginx.Reloads != 1 {
		t.Fatalf("idempotent sync=%#v err=%v", joined, err)
	}
}

func TestApplyRechecksResourceFingerprintAndActiveRevision(t *testing.T) {
	workflow, nginx, _, _ := newFrontingWorkflow(t, passingFrontingHealth)
	preview := frontingPreviewInput()
	desired, _ := GeneratePreview(preview)
	synced, err := workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: "fronting-stale", Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}
	stale := preview
	stale.Resources = append([]hostresources.ProtectableResource(nil), preview.Resources...)
	stale.Resources[0].Fingerprint = strings.Repeat("e", 64)
	if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: stale, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID}); !errors.Is(err, ErrDesiredRevision) {
		t.Fatalf("stale fingerprint err=%v", err)
	}
	nginx.ActiveRevision = strings.Repeat("d", 64)
	nginx.ActiveSHA256 = strings.Repeat("c", 64)
	nginx.RevisionListeners[nginx.ActiveRevision] = []protectionhelper.NginxListener{{Address: "0.0.0.0", Port: 8443}}
	if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID}); !errors.Is(err, ErrActiveRevision) {
		t.Fatalf("stale active err=%v", err)
	}
}

func TestHealthFailureAutomaticallyRestoresExactPreviousRevision(t *testing.T) {
	health := func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, FactCode: "fronting_unavailable"}}
	}
	workflow, nginx, bundles, repository := newFrontingWorkflow(t, health)
	preview := frontingPreviewInput()
	desired, _ := GeneratePreview(preview)
	synced, err := workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: "fronting-health", Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
	if !errors.Is(err, ErrHealthFailed) || result.State != protectionoperations.StateRolledBack || nginx.ActiveRevision != strings.Repeat("a", 64) || nginx.Reloads != 2 || bundles.count != 0 {
		t.Fatalf("result=%#v active=%s reloads=%d bundles=%d err=%v", result, nginx.ActiveRevision, nginx.Reloads, bundles.count, err)
	}
	item, _ := repository.OperationByID(context.Background(), synced.OperationID)
	if item.State != protectionoperations.StateRolledBack {
		t.Fatalf("state=%s", item.State)
	}
}

func TestRollbackFailureCreatesBundleAndTerminalState(t *testing.T) {
	health := func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, FactCode: "fronting_unavailable"}}
	}
	workflow, nginx, bundles, repository := newFrontingWorkflow(t, health)
	preview := frontingPreviewInput()
	desired, _ := GeneratePreview(preview)
	synced, err := workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: "fronting-rollback-failure", Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}
	nginx.Fail[protectionhelper.OperationNginxRestore] = errors.New("typed restore failure")
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
	if err == nil || result.State != protectionoperations.StateRollbackFailed || !result.RecoveryRequired || bundles.count != 1 {
		t.Fatalf("result=%#v bundles=%d err=%v", result, bundles.count, err)
	}
	item, _ := repository.OperationByID(context.Background(), synced.OperationID)
	if item.State != protectionoperations.StateRollbackFailed {
		t.Fatalf("state=%s", item.State)
	}
}

func TestSwitchFailureCancelsBeforeMutationBoundary(t *testing.T) {
	workflow, nginx, _, repository := newFrontingWorkflow(t, passingFrontingHealth)
	preview := frontingPreviewInput()
	desired, _ := GeneratePreview(preview)
	synced, err := workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: "fronting-switch-failure", Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}
	nginx.Fail[protectionhelper.OperationNginxSwitch] = errors.New("typed switch failure")
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
	if !errors.Is(err, ErrSwitchFailed) || result.State != protectionoperations.StateCancelled || nginx.Reloads != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	item, _ := repository.OperationByID(context.Background(), synced.OperationID)
	if item.State != protectionoperations.StateCancelled {
		t.Fatalf("state=%s", item.State)
	}
}

func TestAmbiguousSwitchFailureAfterCommitRollsBackInsteadOfCancelling(t *testing.T) {
	fixture := newFrontingFixture(t, passingFrontingHealth)
	preview := frontingPreviewInput()
	desired, _ := GeneratePreview(preview)
	synced := syncFronting(t, fixture, preview, "fronting-switch-after-commit")
	fixture.nginx.FailAfter[protectionhelper.OperationNginxSwitch] = errors.New("response lost after atomic switch")
	result, err := fixture.workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
	if !errors.Is(err, ErrSwitchFailed) || result.State != protectionoperations.StateRolledBack || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) || fixture.nginx.Reloads != 1 || fixture.bundles.count != 0 {
		t.Fatalf("result=%#v active=%s reloads=%d bundles=%d err=%v", result, fixture.nginx.ActiveRevision, fixture.nginx.Reloads, fixture.bundles.count, err)
	}
}

func TestFrontingCapabilityNegotiationRejectsUnavailableAndUnknown(t *testing.T) {
	available := protectionhelper.DefaultCapabilities()
	available.Nginx = helperinvoker.NewNginx().Support
	for index := range available.Capabilities {
		switch available.Capabilities[index].Operation {
		case protectionhelper.OperationNginxDetectVersion, protectionhelper.OperationNginxValidate, protectionhelper.OperationNginxInstall, protectionhelper.OperationNginxSwitch, protectionhelper.OperationNginxReload, protectionhelper.OperationNginxVerify, protectionhelper.OperationNginxRestore:
			available.Capabilities[index].Available = true
		}
	}
	available.Nginx.ActiveRevision, available.Nginx.ActiveSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64)
	available.Nginx.Listeners = []protectionhelper.NginxListener{{Address: "0.0.0.0", Port: 8443}}
	if !frontingCapabilitiesAvailable(available) {
		t.Fatal("complete typed capability set was rejected")
	}
	unavailable := *available
	unavailable.Nginx.Available = false
	if frontingCapabilitiesAvailable(&unavailable) {
		t.Fatal("unavailable nginx capability was accepted")
	}
	unknown := *available
	unknown.Capabilities = append([]protectionhelper.Capability(nil), available.Capabilities...)
	for index := range unknown.Capabilities {
		if unknown.Capabilities[index].Operation == protectionhelper.OperationNginxVerify {
			unknown.Capabilities[index].Available = false
			unknown.Capabilities[index].Reason = "capability_unknown"
		}
	}
	if frontingCapabilitiesAvailable(&unknown) {
		t.Fatal("unknown active verification capability was accepted")
	}
}

func TestFrontingHealthHasAnOverallBound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := make(chan struct{})
	result := boundedHealth(ctx, func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		<-blocked
		return nil
	}, []hostresources.ProtectableResource{{ID: "target"}}, "fronting_health_timeout")
	close(blocked)
	if len(result) != 1 || result[0].ResourceID != "target" || result[0].Status != componenthealth.StatusDegraded || result[0].FactCode != "fronting_health_timeout" {
		t.Fatalf("bounded health=%#v", result)
	}
}

func TestFrontingHealthFailsClosedOnPanic(t *testing.T) {
	resources := []hostresources.ProtectableResource{{ID: "target"}}
	result := boundedHealth(context.Background(), func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		panic("injected health panic")
	}, resources, "fronting_health_timeout")
	if len(result) != 1 || result[0].ResourceID != "target" || result[0].Status != componenthealth.StatusDegraded || result[0].FactCode != "fronting_health_panic" {
		t.Fatalf("panic health=%#v", result)
	}
}

func TestSyncFailureMatrixCancelsWithoutMutation(t *testing.T) {
	for _, operation := range []protectionhelper.Operation{protectionhelper.OperationNginxValidate, protectionhelper.OperationNginxInstall} {
		t.Run(string(operation), func(t *testing.T) {
			fixture := newFrontingFixture(t, passingFrontingHealth)
			preview := frontingPreviewInput()
			desired, _ := GeneratePreview(preview)
			key := "fronting-sync-failure-" + strings.ReplaceAll(string(operation), ".", "-")
			fixture.nginx.FailSequence[operation] = []error{errors.New("injected typed failure")}
			_, err := fixture.workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: key, Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
			if err == nil {
				t.Fatal("sync failure was accepted")
			}
			item, itemErr := fixture.repository.OperationByIdempotencyKey(context.Background(), key)
			if itemErr != nil || item.State != protectionoperations.StateCancelled || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) || fixture.nginx.Reloads != 0 {
				t.Fatalf("item=%#v active=%s reloads=%d itemErr=%v err=%v", item, fixture.nginx.ActiveRevision, fixture.nginx.Reloads, itemErr, err)
			}
			if operation == protectionhelper.OperationNginxInstall {
				checkpoint, loadErr := fixture.workflow.load(item.OperationID)
				if loadErr != nil || checkpoint.Checkpoint != checkpointValidated || checkpoint.PreviousRevision != strings.Repeat("a", 64) || checkpoint.PreviousSHA256 != strings.Repeat("b", 64) {
					t.Fatalf("previous revision was not durable before install: checkpoint=%#v err=%v", checkpoint, loadErr)
				}
			}
		})
	}
}

func TestApplyFailureMatrixRollsBackAfterMutation(t *testing.T) {
	tests := []struct {
		name   string
		want   error
		inject func(*frontingFixture, string)
	}{
		{name: "reload failure", want: ErrReloadFailed, inject: func(f *frontingFixture, _ string) {
			f.nginx.FailSequence[protectionhelper.OperationNginxReload] = []error{errors.New("reload failed")}
		}},
		{name: "active revision mismatch", want: ErrActiveVerify, inject: func(f *frontingFixture, _ string) {
			f.nginx.FailSequence[protectionhelper.OperationNginxVerify] = []error{errors.New("active revision mismatch")}
		}},
		{name: "listener mismatch", want: ErrActiveVerify, inject: func(f *frontingFixture, desired string) {
			f.nginx.RevisionListeners[desired] = []protectionhelper.NginxListener{{Address: "127.0.0.1", Port: 9443}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFrontingFixture(t, passingFrontingHealth)
			preview := frontingPreviewInput()
			desired, _ := GeneratePreview(preview)
			synced := syncFronting(t, fixture, preview, "fronting-apply-"+strings.ReplaceAll(test.name, " ", "-"))
			test.inject(&fixture, desired.DesiredRevision)
			result, err := fixture.workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
			if !errors.Is(err, test.want) || result.State != protectionoperations.StateRolledBack || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) || fixture.bundles.count != 0 {
				t.Fatalf("result=%#v active=%s bundles=%d err=%v", result, fixture.nginx.ActiveRevision, fixture.bundles.count, err)
			}
		})
	}
}

func TestRollbackReloadAndHealthFailuresCreateRecoveryBundle(t *testing.T) {
	degraded := func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, FactCode: "fronting_unavailable"}}
	}
	for _, test := range []struct {
		name   string
		inject func(*frontingFixture)
		want   error
	}{
		{name: "rollback reload", want: ErrRollbackReload, inject: func(f *frontingFixture) {
			f.nginx.FailSequence[protectionhelper.OperationNginxReload] = []error{nil, errors.New("previous reload failed")}
		}},
		{name: "rollback health", want: ErrRollbackHealth, inject: func(f *frontingFixture) {
			f.workflow.RollbackHealth = degraded
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFrontingFixture(t, degraded)
			preview := frontingPreviewInput()
			desired, _ := GeneratePreview(preview)
			synced := syncFronting(t, fixture, preview, "fronting-"+strings.ReplaceAll(test.name, " ", "-"))
			test.inject(&fixture)
			result, err := fixture.workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
			if !errors.Is(err, test.want) || result.State != protectionoperations.StateRollbackFailed || !result.RecoveryRequired || fixture.bundles.count != 1 {
				t.Fatalf("result=%#v bundles=%d err=%v", result, fixture.bundles.count, err)
			}
		})
	}
}

func TestManualCancellationBeforeAndAfterMutation(t *testing.T) {
	t.Run("before switch", func(t *testing.T) {
		fixture := newFrontingFixture(t, passingFrontingHealth)
		preview := frontingPreviewInput()
		synced := syncFronting(t, fixture, preview, "fronting-cancel-before")
		result, err := fixture.workflow.Rollback(context.Background(), synced.OperationID, "ROLLBACK FRONTING "+synced.OperationID)
		if err != nil || result.State != protectionoperations.StateCancelled || fixture.nginx.Reloads != 0 || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) {
			t.Fatalf("result=%#v reloads=%d active=%s err=%v", result, fixture.nginx.Reloads, fixture.nginx.ActiveRevision, err)
		}
	})
	t.Run("after switch", func(t *testing.T) {
		fixture := newFrontingFixture(t, passingFrontingHealth)
		preview := frontingPreviewInput()
		desired, _ := GeneratePreview(preview)
		synced := syncFronting(t, fixture, preview, "fronting-cancel-after")
		applied, err := fixture.workflow.Apply(context.Background(), ApplyInput{OperationID: synced.OperationID, Preview: preview, DesiredRevision: desired.DesiredRevision, Acknowledged: true, Confirmation: "APPLY FRONTING " + synced.OperationID})
		if err != nil || applied.State != protectionoperations.StateApplied {
			t.Fatalf("apply=%#v err=%v", applied, err)
		}
		result, err := fixture.workflow.Rollback(context.Background(), synced.OperationID, "ROLLBACK FRONTING "+synced.OperationID)
		if err != nil || result.State != protectionoperations.StateRolledBack || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) || fixture.nginx.Reloads != 2 {
			t.Fatalf("result=%#v reloads=%d active=%s err=%v", result, fixture.nginx.Reloads, fixture.nginx.ActiveRevision, err)
		}
	})
}

func TestFrontingLockConflictAndFencingMismatch(t *testing.T) {
	fixture := newFrontingFixture(t, passingFrontingHealth)
	preview := frontingPreviewInput()
	desired, _ := GeneratePreview(preview)
	synced := syncFronting(t, fixture, preview, "fronting-lock-owner")
	_, err := fixture.workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "other", IdempotencyKey: "fronting-lock-conflict", Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
	if !errors.Is(err, protectionoperations.ErrConflict) {
		t.Fatalf("lock conflict err=%v", err)
	}
	operation, _ := fixture.repository.OperationByID(context.Background(), synced.OperationID)
	applying, err := fixture.manager.Transition(context.Background(), operation.OperationID, operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.workflow.Helper.Execute(context.Background(), protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: protectionhelper.Correlation{OperationID: applying.OperationID, InstanceID: fixture.manager.InstanceID(), LockRevision: applying.Revision - 1}, Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{ExpectedRevision: strings.Repeat("a", 64), ExpectedSHA256: strings.Repeat("b", 64), ExpectedBinary: fixture.nginx.Support.Binary, Listeners: []protectionhelper.NginxListener{{Address: "0.0.0.0", Port: 8443}}}})
	if !errors.Is(err, protectionoperations.ErrFenced) {
		t.Fatalf("stale fence err=%v", err)
	}
}

func TestRestartRecoveryAtEveryFrontingCheckpointNeverRepeatsForwardSwitch(t *testing.T) {
	tests := []struct {
		checkpoint       string
		switched         bool
		reloaded         bool
		restored         bool
		rollbackReloaded bool
		actualDesired    bool
	}{
		{checkpoint: checkpointValidated},
		{checkpoint: checkpointSynced},
		{checkpoint: checkpointSwitchIntent},
		{checkpoint: checkpointSwitchIntent, actualDesired: true},
		{checkpoint: checkpointSwitched, switched: true, actualDesired: true},
		{checkpoint: checkpointReloaded, switched: true, reloaded: true, actualDesired: true},
		{checkpoint: checkpointVerified, switched: true, reloaded: true, actualDesired: true},
		{checkpoint: checkpointHealthy, switched: true, reloaded: true, actualDesired: true},
		{checkpoint: checkpointRollbackIntent, switched: true, actualDesired: true},
		{checkpoint: checkpointRestored, switched: true, restored: true},
		{checkpoint: checkpointRollbackReload, switched: true, restored: true, rollbackReloaded: true},
		{checkpoint: checkpointRollbackHealth, switched: true, restored: true, rollbackReloaded: true},
	}
	for index, test := range tests {
		t.Run(fmt.Sprintf("%02d_%s_desired_%t", index, test.checkpoint, test.actualDesired), func(t *testing.T) {
			fixture := newFrontingFixture(t, passingFrontingHealth)
			preview := frontingPreviewInput()
			desired, _ := GeneratePreview(preview)
			synced := syncFronting(t, fixture, preview, fmt.Sprintf("fronting-restart-%02d", index))
			operation, _ := fixture.repository.OperationByID(context.Background(), synced.OperationID)
			if test.checkpoint != checkpointValidated && test.checkpoint != checkpointSynced {
				operation, _ = fixture.manager.Transition(context.Background(), operation.OperationID, operation.Revision, protectionoperations.StateApplying)
				if err := fixture.storage.MarkMutation(operation.OperationID, synced.ArtifactRevision); err != nil {
					t.Fatal(err)
				}
			}
			checkpoint, err := fixture.workflow.load(synced.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint.Switched, checkpoint.Reloaded, checkpoint.Restored, checkpoint.RollbackReloaded = test.switched, test.reloaded, test.restored, test.rollbackReloaded
			if err := fixture.workflow.save(&checkpoint, test.checkpoint); err != nil {
				t.Fatal(err)
			}
			if test.actualDesired {
				fixture.nginx.ActiveRevision, fixture.nginx.ActiveSHA256 = desired.DesiredRevision, desired.GeneratedSHA256
			}
			beforeSwitches := countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch)
			recovery := BackendRecovery{Helper: fixture.workflow.Helper, Manager: fixture.manager, Storage: fixture.storage, Repository: fixture.repository, Health: fixture.workflow.RollbackHealth}
			if err := recovery.AttemptRollback(context.Background(), operation); err != nil {
				t.Fatalf("checkpoint=%s err=%v", test.checkpoint, err)
			}
			if countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != beforeSwitches || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) {
				t.Fatalf("checkpoint=%s calls=%v active=%s", test.checkpoint, fixture.nginx.Calls, fixture.nginx.ActiveRevision)
			}
		})
	}
}

func syncFronting(t *testing.T, fixture frontingFixture, preview PreviewInput, key string) WorkflowResult {
	t.Helper()
	desired, err := GeneratePreview(preview)
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.workflow.Sync(context.Background(), SyncInput{Preview: preview, DesiredRevision: desired.DesiredRevision, Actor: "tester", IdempotencyKey: key, Acknowledged: true, Confirmation: "SYNC FRONTING " + desired.DesiredRevision})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func countOperation(values []protectionhelper.Operation, wanted protectionhelper.Operation) int {
	count := 0
	for _, value := range values {
		if value == wanted {
			count++
		}
	}
	return count
}

func newFrontingWorkflow(t *testing.T, health HealthCheck) (*Workflow, *helperinvoker.Invoker, *bundleRecorder, *protectionrepository.Repository) {
	fixture := newFrontingFixture(t, health)
	return fixture.workflow, fixture.nginx, fixture.bundles, fixture.repository
}

func newFrontingFixture(t *testing.T, health HealthCheck) frontingFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fronting.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repository, protectionoperations.Options{InstanceID: "fronting-test", PID: 77, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	storage, err := protectionartifacts.New(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	root, err := protectionhelper.NewManagedRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	nginx := helperinvoker.NewNginx()
	previous, previousSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	nginx.ActiveRevision, nginx.ActiveSHA256 = previous, previousSHA
	nginx.Revisions[previous] = previousSHA
	nginx.RevisionListeners[previous] = []protectionhelper.NginxListener{{Address: "0.0.0.0", Port: 8443}}
	client, err := protectionhelper.NewClient(root, manager, nginx, frontingAudit{})
	if err != nil {
		t.Fatal(err)
	}
	if health == nil {
		health = passingFrontingHealth
	}
	bundles := &bundleRecorder{}
	workflow := &Workflow{Manager: manager, Helper: client, Artifacts: protectionartifacts.Service{Storage: storage, Store: repository}, Marker: storage, State: storage, Recovery: bundles, Health: health, RollbackHealth: func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		return []componenthealth.Result{{ResourceID: "previous:fronting", Status: componenthealth.StatusOK, FactCode: "listener_ready"}}
	}}
	return frontingFixture{workflow: workflow, nginx: nginx, bundles: bundles, repository: repository, manager: manager, storage: storage}
}

func frontingPreviewInput() PreviewInput {
	fingerprint := strings.Repeat("f", 64)
	resource := hostresources.ProtectableResource{ID: "core:panel:web", Kind: "panel_web", Owner: "core", Name: "Panel", Protocol: "https", Listen: "127.0.0.1", Port: 2053, Fingerprint: fingerprint, Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, AcceptsProxyProtocol: hostresources.CapabilityNo, CanServeFallback: hostresources.CapabilityYes}}
	return PreviewInput{CurrentRevision: strings.Repeat("a", 64), Resources: []hostresources.ProtectableResource{resource}, Routes: []RouteInput{{ResourceID: resource.ID, ResourceRevision: fingerprint, SNI: []string{"panel.example"}, ALPN: []string{"h2"}, Listen: ListenSpec{Address: "0.0.0.0", Port: 8443}}}, FallbackResourceID: resource.ID}
}

func passingFrontingHealth(_ context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
	results := make([]componenthealth.Result, 0, len(resources))
	for _, resource := range resources {
		results = append(results, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusOK, FactCode: "listener_ready"})
	}
	return results
}
