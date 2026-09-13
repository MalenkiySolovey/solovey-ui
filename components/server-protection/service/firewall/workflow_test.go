package firewall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type helperAudit struct{ events []protectionhelper.AuditEvent }

func (a *helperAudit) RecordHelperAudit(_ context.Context, event protectionhelper.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

func TestWorkflowApplyHealthRollbackAndNoSystemBackend(t *testing.T) {
	workflow, mock, manager, repository := newWorkflow(t, func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, Check: "panel", FactCode: "listener_unavailable"}}
	})
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "apply-health-failure", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil || prepared.Joined {
		t.Fatalf("prepare = %#v, %v", prepared, err)
	}
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if !errors.Is(err, ErrHealthFailed) || result.State != protectionoperations.StateRolledBack || !result.RollbackAttempted {
		t.Fatalf("apply result = %#v, %v", result, err)
	}
	item, err := repository.OperationByID(context.Background(), prepared.Operation.OperationID)
	if err != nil || item.State != protectionoperations.StateRolledBack {
		t.Fatalf("operation = %#v, %v", item, err)
	}
	if len(mock.Requests) != 10 || mock.Requests[0].Operation != protectionhelper.OperationCapabilities || mock.Requests[1].Operation != protectionhelper.OperationCapabilities || mock.Requests[2].Operation != protectionhelper.OperationCapabilities || mock.Requests[3].Operation != protectionhelper.OperationNFTValidate || mock.Requests[4].Operation != protectionhelper.OperationCapabilities || mock.Requests[5].Operation != protectionhelper.OperationNFTApply {
		t.Fatalf("mock-only helper sequence = %#v", mock.Requests)
	}
	if mock.Requests[6].Operation != protectionhelper.OperationCapabilities || mock.Requests[7].Operation != protectionhelper.OperationNFTValidate || mock.Requests[8].Operation != protectionhelper.OperationCapabilities || mock.Requests[9].Operation != protectionhelper.OperationNFTRollback {
		t.Fatalf("rollback request missing: %#v", mock.Requests)
	}
	deleteSHA := artifactSHA([]byte("delete table inet solovey_protection\n"))
	if mock.Requests[9].NFTRollback == nil || mock.Requests[9].NFTRollback.ExpectedSHA256 != deleteSHA || mock.Requests[9].NFTRollback.ExpectedCurrentRevision != plan.Revision {
		t.Fatalf("rollback was not fenced to the current semantic empty target: %#v", mock.Requests[9])
	}
	if manager.InstanceID() == "" {
		t.Fatal("fenced manager instance is required")
	}
}

func TestWorkflowPostApplyProtocolHealthFailureRollsBackExactlyOnce(t *testing.T) {
	workflow, mock, _, repository := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "protocol-health-failure", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.Apply(context.Background(), ApplyInput{
		OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources,
		Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID,
		PostApplyHealth: func(context.Context, PostMutationHealthFence) (PostMutationHealthProof, error) {
			return PostMutationHealthProof{}, errors.New("exact UDP transaction failed")
		},
	})
	if !errors.Is(err, ErrHealthFailed) || result.State != protectionoperations.StateRolledBack || !result.RollbackAttempted {
		t.Fatalf("post-apply health result=%#v err=%v", result, err)
	}
	rollbacks := 0
	for _, request := range mock.Requests {
		if request.Operation == protectionhelper.OperationNFTRollback {
			rollbacks++
		}
	}
	if rollbacks != 1 {
		t.Fatalf("rollback calls=%d requests=%#v", rollbacks, mock.Requests)
	}
	operation, err := repository.OperationByID(context.Background(), prepared.Operation.OperationID)
	if err != nil || operation.State != protectionoperations.StateRolledBack {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
}

func TestWorkflowRollbackCancelsPreparedWithoutMutation(t *testing.T) {
	workflow, mock, _, _ := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "cancel-prepared", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	requestsBefore := len(mock.Requests)
	result, err := workflow.Rollback(context.Background(), prepared.Operation.OperationID, "ROLLBACK SERVER PROTECTION "+prepared.Operation.OperationID)
	if err != nil || result.State != protectionoperations.StateCancelled || result.ActualStatus != "ROLLED_BACK" {
		t.Fatalf("prepared cancellation=%#v err=%v", result, err)
	}
	if len(mock.Requests) != requestsBefore {
		t.Fatalf("prepared cancellation invoked helper: before=%d after=%d", requestsBefore, len(mock.Requests))
	}
}

func TestWorkflowIdempotencyUnknownSSHAndRollbackFailure(t *testing.T) {
	workflow, mock, _, repository := newWorkflow(t, nil)
	unknown := BuildPlan([]hostresources.ProtectableResource{{ID: "core:panel:web", Kind: "panel_web", Protocol: "http", Port: 443}}, nil, nil)
	if _, err := workflow.Prepare(context.Background(), PrepareInput{Plan: unknown, Actor: "tester", IdempotencyKey: "unknown-ssh", Confirmation: "PREPARE SERVER PROTECTION " + unknown.Revision}); !errors.Is(err, ErrUnknownSSH) {
		t.Fatalf("unknown SSH error = %v", err)
	}
	plan := applyPlan()
	input := PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "same-key", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision}
	first, err := workflow.Prepare(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := workflow.Prepare(context.Background(), input)
	if err != nil || !second.Joined || second.Operation.OperationID != first.Operation.OperationID {
		t.Fatalf("idempotency = %#v, %v", second, err)
	}
	mock.Responses[protectionhelper.OperationNFTRollback] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeTransportFailed}
	mock.FailAfter[protectionhelper.OperationNFTApply] = errors.New("injected post-mutation apply observation failure")
	bundles := 0
	workflow.Recovery = MockRecovery{Bundle: func(context.Context, protectionrepository.OperationLockModel, string) error { bundles++; return nil }}
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: first.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + first.Operation.OperationID})
	if err == nil || result.State != protectionoperations.StateRollbackFailed || bundles != 1 {
		t.Fatalf("rollback failure = %#v, %v", result, err)
	}
	item, err := repository.OperationByID(context.Background(), first.Operation.OperationID)
	if err != nil || item.State != protectionoperations.StateRollbackFailed {
		t.Fatalf("persisted rollback failure = %#v, %v", item, err)
	}
}

func TestPreflightRejectsStalePayloadAndUnknownListenerProtocol(t *testing.T) {
	empty := BuildPlan(nil, []protectionrepository.PortAllowlistModel{{Protocol: "tcp", PortStart: 22, PortEnd: 22, Reason: "SSH"}}, nil)
	if err := Preflight(empty); !errors.Is(err, ErrUnsafeResource) {
		t.Fatalf("empty protectable resource inventory was accepted: %v", err)
	}
	stale := applyPlan()
	stale.AllowTCPPorts = []int{22}
	if err := Preflight(stale); !errors.Is(err, ErrPlanRevision) {
		t.Fatalf("stale plan payload was accepted: %v", err)
	}
	missingKeep := applyPlan()
	missingKeep.AllowTCPPorts = []int{22}
	missingKeep.Revision = firewallPlanRevision(missingKeep)
	if err := Preflight(missingKeep); !errors.Is(err, ErrUnsafeResource) {
		t.Fatalf("self-consistent plan without the panel keep was accepted: %v", err)
	}
	unknown := BuildPlan(
		[]hostresources.ProtectableResource{{ID: "core:inbound:1", Kind: "inbound", Protocol: "unknown", Port: 443}},
		[]protectionrepository.PortAllowlistModel{{Protocol: "tcp", PortStart: 22, PortEnd: 22, Reason: "SSH"}}, nil,
	)
	if err := Preflight(unknown); !errors.Is(err, ErrUnsafeResource) {
		t.Fatalf("unknown listener protocol was accepted: %v", err)
	}
	unknownKind := BuildPlan(
		[]hostresources.ProtectableResource{{ID: "component:future:listener", Kind: "future_listener", Protocol: "tcp", Port: 8443}},
		[]protectionrepository.PortAllowlistModel{{Protocol: "tcp", PortStart: 22, PortEnd: 22, Reason: "SSH"}}, nil,
	)
	if err := Preflight(unknownKind); !errors.Is(err, ErrUnsafeResource) {
		t.Fatalf("unknown health-check resource kind was accepted: %v", err)
	}
}

func TestWorkflowEmptyOrIncompleteHealthCannotReportApplied(t *testing.T) {
	for name, health := range map[string]HealthCheck{
		"empty": func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result { return nil },
		"wrong-resource": func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
			return []componenthealth.Result{{ResourceID: "unrelated", Status: componenthealth.StatusOK, FactCode: "listener_ready"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			workflow, helper, _, _ := newWorkflow(t, health)
			plan := applyPlan()
			_, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "health-" + name, Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
			if !errors.Is(err, ErrMissingCapability) || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != 0 {
				t.Fatalf("missing health coverage was not rejected before mutation: %v", err)
			}
		})
	}
}

func TestMockRecoveryRollsBackAfterRestartWithoutHostMutation(t *testing.T) {
	workflow, _, manager, repository := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "restart-recovery", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	applying, err := manager.Transition(context.Background(), prepared.Operation.OperationID, prepared.Operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(applying.ExpiresAt+1, 0)
	called := 0
	restarted := protectionoperations.NewManager(repository, protectionoperations.Options{InstanceID: "restarted", PID: 99, Now: func() time.Time { return clock }, PIDProbe: deadPID{}, Recovery: MockRecovery{Rollback: func(context.Context, protectionrepository.OperationLockModel) error { called++; return nil }}})
	results, err := restarted.Recover(context.Background())
	if err != nil || called != 1 || len(results) < 2 || results[len(results)-1].ToState != protectionoperations.StateRolledBack {
		t.Fatalf("restart recovery = %#v, called=%d err=%v", results, called, err)
	}
}

func TestWorkflowPersistsPlanRevisionAndFencesManualRollback(t *testing.T) {
	workflow, _, _, repository := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "manual-rollback", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	changed := BuildPlan([]hostresources.ProtectableResource{{ID: "core:panel:web", Kind: "panel_web", Protocol: "http", Port: 444}}, []protectionrepository.PortAllowlistModel{{Protocol: "tcp", PortStart: 22, PortEnd: 22, Reason: "SSH"}}, nil)
	if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: changed, Resources: changed.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}); !errors.Is(err, ErrPlanRevision) {
		t.Fatalf("revision validation = %v", err)
	}
	applied, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("mock apply = %#v, %v", applied, err)
	}
	rolled, err := workflow.Rollback(context.Background(), prepared.Operation.OperationID, "ROLLBACK SERVER PROTECTION "+prepared.Operation.OperationID)
	if err != nil || rolled.State != protectionoperations.StateRolledBack {
		t.Fatalf("fenced rollback = %#v, %v", rolled, err)
	}
	item, err := repository.OperationByID(context.Background(), prepared.Operation.OperationID)
	if err != nil || item.PlanRevision != plan.Revision || item.State != protectionoperations.StateRolledBack {
		t.Fatalf("persisted workflow state = %#v, %v", item, err)
	}
}

func TestWorkflowManualRollbackRefusesCorruptCheckpointWithoutHelperMutation(t *testing.T) {
	workflow, mock, _, repository := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "corrupt-checkpoint", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}); err != nil {
		t.Fatal(err)
	}
	if err := workflow.State.WriteFirewallState(prepared.Operation.OperationID, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	requestsBefore := len(mock.Requests)
	if _, err := workflow.Rollback(context.Background(), prepared.Operation.OperationID, "ROLLBACK SERVER PROTECTION "+prepared.Operation.OperationID); err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("corrupt checkpoint did not fence rollback: %v", err)
	}
	item, err := repository.OperationByID(context.Background(), prepared.Operation.OperationID)
	if err != nil || item.State != protectionoperations.StateApplied || len(mock.Requests) != requestsBefore {
		t.Fatalf("corrupt checkpoint mutated rollback state or helper: item=%#v requests=%d/%d err=%v", item, requestsBefore, len(mock.Requests), err)
	}
}

func TestWorkflowApplyVerifyMismatchAutomaticallyRollsBack(t *testing.T) {
	workflow, mock, _, repository := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "verify-mismatch", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	mock.Responses[protectionhelper.OperationNFTApply] = protectionhelper.Response{OK: true, NFT: &protectionhelper.NFTResult{AppliedRevision: "stale", CandidateSHA256: "stale", RollbackSHA256: strings.Repeat("a", 64)}}
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if !errors.Is(err, ErrApplyVerify) || result.State != protectionoperations.StateRolledBack || !result.RollbackAttempted {
		t.Fatalf("verify mismatch = %#v, %v", result, err)
	}
	item, loadErr := repository.OperationByID(context.Background(), prepared.Operation.OperationID)
	if loadErr != nil || item.State != protectionoperations.StateRolledBack {
		t.Fatalf("verify rollback state = %#v, %v", item, loadErr)
	}
}

func TestWorkflowCarriesExactValidatedManagedTableFenceIntoApply(t *testing.T) {
	workflow, mock, _, repository := newWorkflow(t, nil)
	firstPlan := applyPlan()
	first, err := workflow.Prepare(context.Background(), PrepareInput{Plan: firstPlan, Actor: "tester", IdempotencyKey: "forward-current-fence-1", Confirmation: "PREPARE SERVER PROTECTION " + firstPlan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workflow.Apply(context.Background(), ApplyInput{OperationID: first.Operation.OperationID, Plan: firstPlan, Resources: firstPlan.Resources, Confirmation: "APPLY SERVER PROTECTION " + first.Operation.OperationID}); err != nil {
		t.Fatal(err)
	}
	authority, err := repository.FirewallAuthority(context.Background())
	if err != nil || !authority.HasComposition {
		t.Fatalf("first authority = %#v, %v", authority, err)
	}
	previousRevision := authority.Composition.ManagedPlanRevision
	secondPlan := BuildPlan(firstPlan.Resources, []protectionrepository.PortAllowlistModel{
		{Protocol: "tcp", PortStart: 22, PortEnd: 22, Reason: "SSH"},
		{Protocol: "tcp", PortStart: 8443, PortEnd: 8443, Reason: "second baseline"},
	}, nil)
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: secondPlan, Actor: "tester", IdempotencyKey: "forward-current-fence-2", Confirmation: "PREPARE SERVER PROTECTION " + secondPlan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	requestsBefore := len(mock.Requests)
	result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: secondPlan, Resources: secondPlan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil || result.State != protectionoperations.StateApplied {
		t.Fatalf("fenced apply = %#v, %v", result, err)
	}
	for _, request := range mock.Requests[requestsBefore:] {
		if request.Operation != protectionhelper.OperationNFTApply {
			continue
		}
		if request.NFTApply == nil || !request.NFTApply.ExpectedPreviousTablePresent || request.NFTApply.ExpectedPreviousRevision != previousRevision || request.NFTApply.ExpectedPreviousSemanticSHA256 == "" {
			t.Fatalf("apply did not carry the exact validate-time table fence: %#v", request)
		}
		return
	}
	t.Fatal("fenced nft apply request was not emitted")
}

func TestWorkflowRollbackHealthFailureBecomesRollbackFailed(t *testing.T) {
	workflow, _, _, _ := newWorkflow(t, nil)
	workflow.RollbackHealth = func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, FactCode: "listener_unavailable"}}
	}
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "rollback-health", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("apply = %#v, %v", applied, err)
	}
	result, err := workflow.Rollback(context.Background(), prepared.Operation.OperationID, "ROLLBACK SERVER PROTECTION "+prepared.Operation.OperationID)
	if !errors.Is(err, ErrRollbackHealth) || result.State != protectionoperations.StateRollbackFailed {
		t.Fatalf("rollback health = %#v, %v", result, err)
	}
}

func TestSQLiteAuthoritySurvivesPhysicalRuntimeRootLossAndRetiresOnRestart(t *testing.T) {
	workflow, mock, manager, repository, rootPath := newWorkflowWithRoot(t, nil)
	plan := applyPlan()
	applied := applyCompositionWorkflowPlan(t, &workflow, plan, "sqlite-runtime-loss-before-reboot")
	applyCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply)
	if err := manager.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(rootPath); err != nil {
		t.Fatal(err)
	}
	mock.ManagedTablePresent, mock.ManagedPlanRevision, mock.ManagedCandidateSHA, mock.ManagedCandidateSemantic, mock.ManagedTimedMembership = false, "", "", "", ""

	storage, err := protectionartifacts.NewWithRecoveryProjection(rootPath, sptest.SystemdRecoveryProjection())
	if err != nil {
		t.Fatal(err)
	}
	restarted := protectionoperations.NewManager(repository, protectionoperations.Options{
		InstanceID: "sqlite-runtime-loss-restart", PID: 78,
		Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil },
	})
	t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
	client, err := protectionhelper.NewClient(sptest.ManagedRoot(t, rootPath), restarted, mock, &helperAudit{})
	if err != nil {
		t.Fatal(err)
	}
	workflow.Manager, workflow.Helper = restarted, client
	workflow.Artifacts = protectionartifacts.Service{Storage: storage, Store: repository}
	workflow.Marker, workflow.State = storage, storage
	if err := restarted.SetReconcilerForKind(protectionoperations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	retired, err := repository.OperationByID(t.Context(), applied.OperationID)
	if err != nil || retired.State != protectionoperations.StateForgotten {
		t.Fatalf("durable operation was not retired after volatile loss: operation=%#v err=%v", retired, err)
	}
	authority, err := repository.FirewallAuthority(t.Context())
	if err != nil || authority.HasComposition || len(authority.Contributions) != 0 || !authority.HasObservation || authority.Observation.State != FirewallLiveAbsent || authority.Observation.HasCommittedAuthority {
		t.Fatalf("durable authority remained mixed after volatile loss: authority=%#v err=%v", authority, err)
	}
	if got := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply); got != applyCalls {
		t.Fatalf("restart replayed a candidate: apply calls %d -> %d", applyCalls, got)
	}
	rollbackCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTRollback)
	if result, rollbackErr := workflow.Rollback(t.Context(), applied.OperationID, "ROLLBACK SERVER PROTECTION "+applied.OperationID); rollbackErr == nil || result.RollbackAttempted || helperOperationCount(mock.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls {
		t.Fatalf("lost rollback artifact remained presented: result=%#v err=%v", result, rollbackErr)
	}

	nextPlan := compositionBaselineWithTCPPort(plan, 9443)
	next := applyCompositionWorkflowPlan(t, &workflow, nextPlan, "sqlite-runtime-loss-after-reboot")
	if next.State != protectionoperations.StateApplied || helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply) != applyCalls+1 {
		t.Fatalf("next apply could not establish a fresh generation: operation=%#v", next)
	}
}

type deadPID struct{}

func (deadPID) Alive(int) (bool, error) { return false, nil }

func newWorkflow(t *testing.T, health HealthCheck) (Workflow, *testHelperInvoker, *protectionoperations.Manager, *protectionrepository.Repository) {
	workflow, helper, manager, repository, _ := newWorkflowWithRoot(t, health)
	return workflow, helper, manager, repository
}

type workflowDatabaseConfig struct {
	Suffix    string
	Configure func(*gorm.DB)
	Open      func(string) (*gorm.DB, error)
}

func newWorkflowWithRoot(t *testing.T, health HealthCheck, configs ...workflowDatabaseConfig) (Workflow, *testHelperInvoker, *protectionoperations.Manager, *protectionrepository.Repository, string) {
	t.Helper()
	suffix := ""
	if len(configs) != 0 {
		suffix = configs[0].Suffix
	}
	open := func(path string) (*gorm.DB, error) { return gorm.Open(sqlite.Open(path+suffix), &gorm.Config{}) }
	if len(configs) != 0 && configs[0].Open != nil {
		open = configs[0].Open
	}
	db, err := open(filepath.Join(t.TempDir(), "workflow.db"))
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
	for _, setup := range configs {
		if setup.Configure != nil {
			setup.Configure(db)
		}
	}
	repository := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repository, protectionoperations.Options{InstanceID: "workflow", PID: 77, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	storage, err := protectionartifacts.NewWithRecoveryProjection(rootPath, sptest.SystemdRecoveryProjection())
	if err != nil {
		t.Fatal(err)
	}
	root := sptest.ManagedRoot(t, rootPath)
	capabilities := protectionhelper.DefaultCapabilities()
	capabilities.NFT = protectionhelper.NFTSupport{PlatformKnown: true, Linux: true, Available: true}
	for i := range capabilities.Capabilities {
		switch capabilities.Capabilities[i].Operation {
		case protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTObserve, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback:
			capabilities.Capabilities[i].Available = true
			capabilities.Capabilities[i].Reason = ""
		}
	}
	mock := newTestHelperInvoker(capabilities)
	for _, operation := range []protectionhelper.Operation{protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTObserve, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback} {
		mock.Responses[operation] = protectionhelper.Response{OK: true}
	}
	client, err := protectionhelper.NewClient(root, manager, mock, &helperAudit{})
	if err != nil {
		t.Fatal(err)
	}
	if health == nil {
		health = passingHealth
	}
	return Workflow{
		Manager: manager, Helper: client, Artifacts: protectionartifacts.Service{Storage: storage, Store: repository}, Marker: storage, State: storage,
		Recovery: MockRecovery{}, Health: health,
		RollbackHealth: func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
			return []componenthealth.Result{{ResourceID: "rollback:panel", Status: componenthealth.StatusOK, FactCode: "listener_ready"}}
		},
		Contributions: repository, Now: func() time.Time { return time.Unix(1000, 0).UTC() },
	}, mock, manager, repository, rootPath
}

func passingHealth(_ context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
	results := make([]componenthealth.Result, 0, len(resources))
	for _, resource := range resources {
		results = append(results, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusOK, FactCode: "listener_ready"})
	}
	return results
}

func applyPlan() FirewallPlan {
	return BuildPlan([]hostresources.ProtectableResource{{ID: "core:panel:web", Kind: "panel_web", Protocol: "http", Port: 443}}, []protectionrepository.PortAllowlistModel{{Protocol: "tcp", PortStart: 22, PortEnd: 22, Reason: "SSH"}}, nil)
}
