// Package actions describes existing owner commands and binds their existing
// authorization policy to the actual owner input. It executes no command and
// creates no authentication, session, token, role or approval store.
package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

const (
	BackupRestore                 = "backup.restore"
	ComponentRemove               = "component.remove"
	ProviderMaintenance           = "update.provider-maintenance"
	PermissionBackupRestore       = "backup.restore"
	PermissionComponentUninstall  = "component.uninstall"
	PermissionProviderMaintenance = "update.maintain"
)

var ErrDenied = errors.New("semantic_action_denied")

// Descriptor is metadata for an existing semantic owner, never an executor.
// The fixed catalog only describes migrated commands with real consumers.
type Descriptor struct {
	ID            string `json:"id"`
	Owner         string `json:"owner"`
	Permission    string `json:"permission"`
	HostScope     string `json:"hostScope"`
	ResourceScope string `json:"resourceScope"`
	RiskClass     string `json:"riskClass"`
	InputSchema   string `json:"inputSchema"`
	Preview       bool   `json:"preview"`
	StepUpPolicy  string `json:"stepUpPolicy"`
	AuditClass    string `json:"auditClass"`
	ComponentID   string `json:"componentId,omitempty"`
}

func Lookup(id string) (Descriptor, bool) {
	switch id {
	case BackupRestore:
		return Descriptor{ID: id, Owner: "data", Permission: PermissionBackupRestore,
			HostScope: "LOCAL", ResourceScope: "database", RiskClass: "CRITICAL",
			InputSchema: "solovey.action/backup.restore/v1", Preview: true,
			StepUpPolicy: "existing_browser_one_use_or_verified_startup_recovery", AuditClass: "db_imported"}, true
	case ComponentRemove:
		return Descriptor{ID: id, Owner: "component-lifecycle", Permission: PermissionComponentUninstall,
			HostScope: "LOCAL", ResourceScope: "component:<id>", RiskClass: "HIGH",
			InputSchema:  "solovey.action/component.remove/v1",
			StepUpPolicy: "existing_browser_one_use", AuditClass: "component_installed_changed", ComponentID: "panel-update-ui"}, true
	case ProviderMaintenance:
		return Descriptor{ID: id, Owner: "update", Permission: PermissionProviderMaintenance,
			HostScope: "LOCAL", ResourceScope: "panel", RiskClass: "CRITICAL",
			InputSchema: "solovey.action/update.provider-maintenance/v1", Preview: true,
			StepUpPolicy: "existing_browser_one_use", AuditClass: "core_provider_maintenance_requested"}, true
	}
	return Descriptor{}, false
}

func KnownPermission(permission string) bool {
	return permission == PermissionBackupRestore || permission == PermissionComponentUninstall || permission == PermissionProviderMaintenance
}

// ComponentAllowed requires both catalog ownership and an actual manifest
// declaration. Callers obtain these from the registered ComponentHost route.
func ComponentAllowed(componentID string, declared []string, actionID string) bool {
	descriptor, known := Lookup(actionID)
	if !known || componentID == "" || descriptor.ComponentID != componentID {
		return false
	}
	for _, id := range declared {
		if id == actionID {
			return true
		}
	}
	return false
}

// Invocation reuses the shared Host/owner/resource/operation correlation tuple.
// Target keeps the current browser step-up target, including the restore plan
// digest. It is reconstructed from typed input by the semantic owner.
type Invocation struct {
	ID          string
	InputSchema string
	InputDigest string
	Target      string
	Reference   operationcoordination.Reference
}

type RestoreInput struct{ RehearsalRevision string }
type ComponentRemoveInput struct{ ComponentID string }
type ProviderMaintenanceInput struct{ PlanDigest string }

// PlanDigest is reconstructed by Update from its full typed maintenance input,
// including source/configuration revisions and the exact artifact selection.
func MaintainProvider(reference operationcoordination.Reference, input ProviderMaintenanceInput) Invocation {
	descriptor, _ := Lookup(ProviderMaintenance)
	return Invocation{ID: descriptor.ID, InputSchema: descriptor.InputSchema,
		InputDigest: inputDigest(descriptor.InputSchema, input.PlanDigest),
		Target:      "provider-maintenance:" + input.PlanDigest, Reference: reference}
}

func Restore(reference operationcoordination.Reference, input RestoreInput) Invocation {
	descriptor, _ := Lookup(BackupRestore)
	return Invocation{ID: descriptor.ID, InputSchema: descriptor.InputSchema,
		InputDigest: inputDigest(descriptor.InputSchema, input.RehearsalRevision),
		Target:      "database:restore:" + input.RehearsalRevision, Reference: reference}
}

func RemoveComponent(reference operationcoordination.Reference, input ComponentRemoveInput) Invocation {
	descriptor, _ := Lookup(ComponentRemove)
	return Invocation{ID: descriptor.ID, InputSchema: descriptor.InputSchema,
		InputDigest: inputDigest(descriptor.InputSchema, input.ComponentID),
		Target:      "component:" + input.ComponentID, Reference: reference}
}

func (i Invocation) Validate() error {
	descriptor, known := Lookup(i.ID)
	r := i.Reference
	if !known || !KnownPermission(descriptor.Permission) || i.InputSchema != descriptor.InputSchema ||
		!r.HostRef.IsLocal() || r.Owner != descriptor.Owner || r.ResourceID == "unavailable" || r.OperationID == "unavailable" ||
		r != operationcoordination.NewReference(r.Owner, r.ResourceID, r.OperationID) {
		return ErrDenied
	}
	switch i.ID {
	case BackupRestore:
		revision, ok := strings.CutPrefix(i.Target, "database:restore:")
		if !ok || len(revision) != 64 || strings.Trim(revision, "0123456789abcdef") != "" ||
			r.ResourceID != "database" || i.InputDigest != inputDigest(i.InputSchema, revision) {
			return ErrDenied
		}
	case ComponentRemove:
		id, ok := strings.CutPrefix(i.Target, "component:")
		if !ok || id == "" || len(id) > 128 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" ||
			r.ResourceID != i.Target || i.InputDigest != inputDigest(i.InputSchema, id) {
			return ErrDenied
		}
	case ProviderMaintenance:
		digest, ok := strings.CutPrefix(i.Target, "provider-maintenance:")
		if !ok || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" ||
			r.ResourceID != "panel" || i.InputDigest != inputDigest(i.InputSchema, digest) {
			return ErrDenied
		}
	default:
		return ErrDenied
	}
	return nil
}

// Decision is an in-process result from the existing authority adapter, never
// a reusable client token or a replacement for a one-use step-up grant.
type Decision struct {
	Allowed    bool
	Actor      string
	Permission string
	Invocation Invocation
}

type Authorizer func(context.Context, Invocation) (Decision, error)

// Require is called by the owner before mutation. The policy sees actual input;
// missing/unknown/denied/mismatched policy results always fail closed.
func Require(ctx context.Context, invocation Invocation, authorize Authorizer) (Decision, error) {
	if ctx == nil || ctx.Err() != nil || authorize == nil || invocation.Validate() != nil {
		return Decision{}, ErrDenied
	}
	descriptor, _ := Lookup(invocation.ID)
	decision, err := authorize(ctx, invocation)
	if err != nil || ctx.Err() != nil || !decision.Allowed || decision.Actor == "" || !KnownPermission(decision.Permission) ||
		decision.Permission != descriptor.Permission || decision.Invocation != invocation {
		return Decision{}, ErrDenied
	}
	return decision, nil
}

func inputDigest(schema, input string) string {
	digest := sha256.Sum256([]byte(schema + "\x00" + input))
	return hex.EncodeToString(digest[:])
}
