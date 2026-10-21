package service

import (
	"context"
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"testing"
)

func TestRemovalOwnerDeniesMissingOrMismatchedPolicyBeforeMetadataAdmission(t *testing.T) {
	for _, variant := range []string{"missing", "denied", "unknown_permission", "different_component"} {
		t.Run(variant, func(t *testing.T) {
			reconciled, authorized := false, false
			manager := RuntimeManager{Reconcile: func(context.Context) error { reconciled = true; return nil }}
			operation := OperationContext{RequestContext: t.Context()}
			if variant != "missing" {
				operation.Authorize = func(_ context.Context, actual actions.Invocation) (actions.Decision, error) {
					authorized = true
					if actual.ID != actions.ComponentRemove || actual.Target != "component:telegram" ||
						actual.Reference.ResourceID != "component:telegram" || !actual.Reference.HostRef.IsLocal() {
						t.Fatalf("owner did not bind actual removal target: %+v", actual)
					}
					decision := actions.Decision{Allowed: true, Actor: "synthetic-admin", Permission: actions.PermissionComponentUninstall, Invocation: actual}
					switch variant {
					case "denied":
						decision.Allowed = false
					case "unknown_permission":
						decision.Permission = "future.permission"
					case "different_component":
						decision.Invocation.Target = "component:observability-extra"
					}
					return decision, nil
				}
			}
			status, err := manager.Remove(operation, "telegram")
			if !errors.Is(err, actions.ErrDenied) || status.ID != "" || reconciled || authorized != (variant != "missing") {
				t.Fatalf("rejected removal reached admission: %v %+v reconciled=%v", err, status, reconciled)
			}
		})
	}
}

// Existing lifecycle/removal fixtures declare their synthetic authority
// explicitly. This helper is test-only and cannot become a production bypass.
func authorizedRemovalContext(ctx context.Context) OperationContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return OperationContext{RequestContext: ctx, Authorize: func(_ context.Context, actual actions.Invocation) (actions.Decision, error) {
		return actions.Decision{Allowed: true, Actor: "fixture-admin", Permission: actions.PermissionComponentUninstall, Invocation: actual}, nil
	}}
}
