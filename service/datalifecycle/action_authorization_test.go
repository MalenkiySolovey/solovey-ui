package datalifecycle

import (
	"context"
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestRestoreOwnerDeniesBeforeMaintenanceRehearsalOrDatabaseMutation(t *testing.T) {
	for _, variant := range []string{"missing", "denied", "unknown_permission", "different_plan"} {
		t.Run(variant, func(t *testing.T) {
			manager := NewManager()
			manager.DB = func() *gorm.DB { t.Fatal("authorization rejection reached database"); return nil }
			request := RestoreRequest{ExpectedRehearsalRevision: strings.Repeat("a", 64),
				IdempotencyKey: "restore-action-policy-test", Acknowledged: true, Source: strings.NewReader("not reached")}
			request.Confirmation = RestoreConfirmation(request.ExpectedRehearsalRevision)
			called := false
			if variant != "missing" {
				request.Authorize = func(_ context.Context, actual actions.Invocation) (actions.Decision, error) {
					called = true
					if actual.ID != actions.BackupRestore || actual.Target != "database:restore:"+request.ExpectedRehearsalRevision ||
						actual.Reference.ResourceID != "database" || actual.Reference.Owner != "data" || !actual.Reference.HostRef.IsLocal() {
						t.Fatalf("owner did not bind its actual input: %+v", actual)
					}
					decision := actions.Decision{Allowed: true, Actor: "synthetic-admin", Permission: actions.PermissionBackupRestore, Invocation: actual}
					switch variant {
					case "denied":
						decision.Allowed = false
					case "unknown_permission":
						decision.Permission = "future.permission"
					case "different_plan":
						decision.Invocation = actions.Restore(actual.Reference, actions.RestoreInput{RehearsalRevision: strings.Repeat("b", 64)})
					}
					return decision, nil
				}
			}
			operation, _, err := manager.ExecuteRestore(t.Context(), request)
			if !errors.Is(err, actions.ErrDenied) || operation.OperationID != "" || called != (variant != "missing") {
				t.Fatalf("rejected owner action: %+v %v called=%v", operation, err, called)
			}
		})
	}
}
