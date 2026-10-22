// Package managementcontract projects existing Panel owners into the public
// contract. It is not a dispatcher, listener or new authorization authority.
package managementcontract

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func LocalHost() wire.HostRef {
	local := hostref.Local()
	return wire.HostRef{Kind: local.Kind, ID: local.ID}
}

// Action preserves current owner metadata. TARGET_HOST describes semantic
// scope; the Panel invocation adapter below still admits only actual Local.
func Action(id string) (wire.ActionDescriptor, bool) {
	descriptor, exists := actions.Lookup(id)
	if !exists || !actions.KnownPermission(descriptor.Permission) || descriptor.HostScope != "LOCAL" {
		return wire.ActionDescriptor{}, false
	}
	result := wire.ActionDescriptor{ID: descriptor.ID, Owner: descriptor.Owner, Permission: descriptor.Permission,
		HostScope: "TARGET_HOST", ResourceScope: descriptor.ResourceScope, RiskClass: descriptor.RiskClass,
		InputSchema: descriptor.InputSchema, Preview: descriptor.Preview, StepUpPolicy: descriptor.StepUpPolicy,
		AuditClass: descriptor.AuditClass, RevisionFence: "OWNER_LIFECYCLE"}
	if descriptor.ComponentID != "" {
		result.ComponentID = &descriptor.ComponentID
	}
	if descriptor.ID == actions.BackupRestore {
		result.RevisionFence = "OWNER_REQUIRED"
	}
	return result, true
}

func Invocation(command wire.Command) (actions.Invocation, error) {
	if err := wire.ValidateCommand(command); err != nil {
		return actions.Invocation{}, err
	}
	if command.Host != LocalHost() {
		return actions.Invocation{}, actions.ErrDenied
	}
	reference := operationcoordination.NewReference(command.Resource.Owner, command.Resource.ResourceID, command.CommandID)
	var invocation actions.Invocation
	switch command.ActionID {
	case actions.BackupRestore:
		var input wire.RestoreInput
		if err := json.Unmarshal(command.Input, &input); err != nil {
			return actions.Invocation{}, err
		}
		invocation = actions.Restore(reference, actions.RestoreInput{RehearsalRevision: string(input.RehearsalRevision)})
	case actions.ComponentRemove:
		// Current removal has its owner's lifecycle fence, not an exposed
		// optimistic revision. Never pretend an unused caller fence was checked.
		if command.ExpectedRevision != nil {
			return actions.Invocation{}, errors.New("UNSUPPORTED")
		}
		var input wire.ComponentRemoveInput
		if err := json.Unmarshal(command.Input, &input); err != nil {
			return actions.Invocation{}, err
		}
		invocation = actions.RemoveComponent(reference, actions.ComponentRemoveInput{ComponentID: input.ComponentID})
	default:
		return actions.Invocation{}, actions.ErrDenied
	}
	if invocation.InputDigest != string(command.InputDigest) || invocation.Validate() != nil {
		return actions.Invocation{}, actions.ErrDenied
	}
	return invocation, nil
}

// Require delegates to the existing owner action policy. Decoded grant metadata
// cannot be passed as an Authorizer and is never itself accepted as authority.
func Require(ctx context.Context, command wire.Command, authorize actions.Authorizer) (actions.Decision, error) {
	invocation, err := Invocation(command)
	if err != nil {
		return actions.Decision{}, err
	}
	return actions.Require(ctx, invocation, authorize)
}
