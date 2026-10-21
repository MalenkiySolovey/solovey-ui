package openwrt

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"strings"
	"testing"
)

func TestPreservationAuthorityOnlyAcceptsTheVerifiedRestoreRevision(t *testing.T) {
	revision := strings.Repeat("a", 64)
	reference := operationcoordination.NewReference("data", "database", "data-operation:preservation")
	policy := authorizeVerifiedPreservation(revision)
	invocation := actions.Restore(reference, actions.RestoreInput{RehearsalRevision: revision})
	decision, err := actions.Require(t.Context(), invocation, policy)
	if err != nil || decision.Actor != "system:openwrt-preservation" {
		t.Fatalf("verified recovery rejected: %+v %v", decision, err)
	}
	changed := actions.Restore(reference, actions.RestoreInput{RehearsalRevision: strings.Repeat("b", 64)})
	if _, err := actions.Require(t.Context(), changed, policy); !errors.Is(err, actions.ErrDenied) {
		t.Fatal("preservation authority accepted another snapshot")
	}
	removal := actions.RemoveComponent(operationcoordination.NewReference("component-lifecycle", "component:telegram", "component-remove:test"), actions.ComponentRemoveInput{ComponentID: "telegram"})
	if _, err := actions.Require(t.Context(), removal, policy); !errors.Is(err, actions.ErrDenied) {
		t.Fatal("startup restore authority admitted another action")
	}
}
