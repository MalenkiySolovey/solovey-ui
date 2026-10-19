package firewall

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"
)

func TestNetworkPlannerChecksCurrentInputsAfterCandidateValidation(t *testing.T) {
	workflow, adapter, _, _ := newWorkflow(t, nil)
	plan := applyPlan()
	reads := 0
	workflow.CurrentNetworkInputs = func(_ context.Context, desired FirewallPlan) (FirewallPlan, error) {
		reads++
		if reads == 2 {
			desired.InputRevision = strings.Repeat("f", 64)
		}
		return desired, nil
	}
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "network-stale", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.Apply(t.Context(), ApplyInput{Plan: plan, OperationID: prepared.Operation.OperationID, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	var conflict *networkplan.Conflict
	if !errors.As(err, &conflict) || conflict.Reason != networkplan.StalePlan || result.State != operations.StatePrepared {
		t.Fatalf("stale apply = %#v, %v", result, err)
	}
	validated := false
	for _, request := range adapter.Requests {
		if request.Operation == helper.OperationNFTValidate {
			validated = true
		}
		if request.Operation == helper.OperationNFTApply || request.Operation == helper.OperationNFTRollback {
			t.Fatal("changed inputs reached mutation")
		}
	}
	if !validated || reads != 2 {
		t.Fatalf("did not test validation-to-apply boundary: validated=%v reads=%d", validated, reads)
	}
}

func TestNetworkPlannerResultUsesR3EvidenceAndRestartProjection(t *testing.T) {
	workflow, _, _, repository := newWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: plan, Actor: "tester", IdempotencyKey: "network-r3", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.Apply(t.Context(), ApplyInput{Plan: plan, OperationID: prepared.Operation.OperationID, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err != nil || result.NetworkPlanRevision == "" || len(result.NetworkResources) == 0 || len(result.NetworkEvidence) != 2 {
		t.Fatalf("apply = %#v, %v", result, err)
	}
	for index, proof := range []componenthealth.ProofKind{componenthealth.ConfigValid, componenthealth.Applied} {
		if !result.NetworkEvidence[index].Satisfies(componenthealth.Requirement{Owner: "server-protection", Source: "nft-adapter", ProofKind: proof, DesiredRevision: result.NetworkPlanRevision, MaxAge: 30 * time.Second}, workflow.now()) {
			t.Fatalf("unbound network evidence: %#v", result.NetworkEvidence)
		}
	}
	if result.NetworkEvidence[0].Satisfies(componenthealth.Requirement{Owner: "server-protection", Source: "nft-adapter", ProofKind: componenthealth.EndToEndVerified, DesiredRevision: result.NetworkPlanRevision, MaxAge: 30 * time.Second}, workflow.now()) {
		t.Fatal("syntax evidence promoted to end-to-end proof")
	}
	row, err := repository.OperationByID(t.Context(), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	change, job := ProjectOperation(row)
	if !reflect.DeepEqual(*result.Change, change) || !reflect.DeepEqual(*result.Job, job) || change.Outcome != operationcoordination.OutcomeCommitted || job.Progress != nil {
		t.Fatalf("restart projection differs: %#v %#v", change, job)
	}
}
