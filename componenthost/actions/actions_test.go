package actions

import (
	"context"
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"strings"
	"testing"
)

func restoreInvocation() Invocation {
	return Restore(operationcoordination.NewReference("data", "database", "data-operation:test"), RestoreInput{RehearsalRevision: strings.Repeat("a", 64)})
}
func allowActual(_ context.Context, invocation Invocation) (Decision, error) {
	descriptor, _ := Lookup(invocation.ID)
	return Decision{Allowed: true, Actor: "test-admin", Permission: descriptor.Permission, Invocation: invocation}, nil
}

func TestCatalogContainsTwoDifferentRealOwnersAndReturnsImmutableValues(t *testing.T) {
	restore, _ := Lookup(BackupRestore)
	remove, _ := Lookup(ComponentRemove)
	if restore.Owner == remove.Owner || restore.InputSchema == remove.InputSchema || !restore.Preview || remove.Preview ||
		!KnownPermission(restore.Permission) || !KnownPermission(remove.Permission) {
		t.Fatal("invalid migrated action catalog")
	}
	restore.Permission = "unknown"
	stored, _ := Lookup(BackupRestore)
	if stored.Permission != PermissionBackupRestore {
		t.Fatal("caller mutated catalog")
	}
	if _, known := Lookup("future.execute"); known || KnownPermission("admin") || KnownPermission("future.permission") {
		t.Fatal("unknown action/permission admitted")
	}
	if !ComponentAllowed("panel-update-ui", []string{ComponentRemove}, ComponentRemove) ||
		ComponentAllowed("panel-update-ui", nil, ComponentRemove) ||
		ComponentAllowed("telegram", []string{ComponentRemove}, ComponentRemove) ||
		ComponentAllowed("panel-update-ui", []string{BackupRestore}, BackupRestore) {
		t.Fatal("component declaration/ownership boundary is not enforced")
	}
}

func TestOwnerPolicyRequiresActualKnownHostResourceAndTypedInput(t *testing.T) {
	valid := restoreInvocation()
	if _, err := Require(t.Context(), valid, allowActual); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Invocation){
		"unknown action": func(i *Invocation) { i.ID = "future.execute" },
		"schema":         func(i *Invocation) { i.InputSchema = "future" },
		"digest":         func(i *Invocation) { i.InputDigest = strings.Repeat("b", 64) },
		"plan":           func(i *Invocation) { i.Target = "database:restore:" + strings.Repeat("b", 64) },
		"host":           func(i *Invocation) { i.Reference.HostRef = hostref.Ref{Kind: "REMOTE", ID: "foreign"} },
		"owner": func(i *Invocation) {
			i.Reference = operationcoordination.NewReference("foreign", "database", "data-operation:test")
		},
		"resource": func(i *Invocation) {
			i.Reference = operationcoordination.NewReference("data", "other", "data-operation:test")
		},
		"correlation": func(i *Invocation) { i.Reference.ChangeID = "forged" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			invocation, called := valid, false
			mutate(&invocation)
			_, err := Require(t.Context(), invocation, func(context.Context, Invocation) (Decision, error) { called = true; return Decision{}, nil })
			if !errors.Is(err, ErrDenied) || called {
				t.Fatalf("invalid invocation reached authorization: %v %v", err, called)
			}
		})
	}
	removed := RemoveComponent(operationcoordination.NewReference("component-lifecycle", "component:telegram", "component-remove:test"), ComponentRemoveInput{ComponentID: "telegram"})
	if _, err := Require(t.Context(), removed, allowActual); err != nil {
		t.Fatal(err)
	}
	removed.Target = "component:observability-extra"
	if _, err := Require(t.Context(), removed, allowActual); !errors.Is(err, ErrDenied) {
		t.Fatal("changed remove target admitted")
	}
}

func TestMissingDeniedOrMismatchedDecisionFailsClosed(t *testing.T) {
	invocation := restoreInvocation()
	if _, err := Require(t.Context(), invocation, nil); !errors.Is(err, ErrDenied) {
		t.Fatal("missing policy admitted")
	}
	mutations := map[string]func(*Decision){
		"missing":            func(d *Decision) { *d = Decision{} },
		"denied":             func(d *Decision) { d.Allowed = false },
		"actor":              func(d *Decision) { d.Actor = "" },
		"unknown permission": func(d *Decision) { d.Permission = "future" },
		"wrong permission":   func(d *Decision) { d.Permission = PermissionComponentUninstall },
		"stale plan": func(d *Decision) {
			d.Invocation = Restore(invocation.Reference, RestoreInput{RehearsalRevision: strings.Repeat("b", 64)})
		},
		"other operation": func(d *Decision) {
			d.Invocation.Reference = operationcoordination.NewReference("data", "database", "data-operation:other")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			_, err := Require(t.Context(), invocation, func(ctx context.Context, actual Invocation) (Decision, error) {
				decision, _ := allowActual(ctx, actual)
				mutate(&decision)
				return decision, nil
			})
			if !errors.Is(err, ErrDenied) {
				t.Fatal("mismatched decision admitted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	_, err := Require(ctx, invocation, func(ctx context.Context, actual Invocation) (Decision, error) {
		cancel()
		return allowActual(ctx, actual)
	})
	if !errors.Is(err, ErrDenied) {
		t.Fatal("cancelled authorization admitted")
	}
}
