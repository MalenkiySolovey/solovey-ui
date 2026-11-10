package runtime

import (
	"context"
	"errors"

	hostcore "github.com/MalenkiySolovey/solovey-host/coreruntime"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/sagernet/sing-box/daemon"
)

// SelectRuntimeGroupWithEvidence is the existing runtime-only owner operation.
// Transport scopes are authorized by the caller before entering CoreRuntime.
// Provider credentials and lifecycle/mutation locks never leave this adapter.
func (c *Core) SelectRuntimeGroupWithEvidence(ctx context.Context, generation, group, member string) (hostcore.SelectionResult, error) {
	if generation == "" {
		return hostcore.SelectionResult{}, ErrStaleGeneration
	}
	if c == nil {
		return hostcore.SelectionResult{}, ErrCoreUnavailable
	}
	// Preserve the existing unavailable/stale-generation error precedence.
	// The execution lease rechecks this identity before any provider operation.
	c.access.RLock()
	api, running := c.privateAPI, c.isRunning
	if !running || api == nil {
		c.access.RUnlock()
		return hostcore.SelectionResult{}, ErrCoreUnavailable
	}
	currentGeneration := api.generation
	c.access.RUnlock()
	if generation != currentGeneration {
		return hostcore.SelectionResult{}, ErrStaleGeneration
	}
	identity := c.selectionIdentity()
	if !coreprovider.SourceIdentityVerified(identity) {
		return hostcore.SelectionResult{}, ErrRuntimeAPIUnavailable
	}
	host := hostref.Local()
	intent := hostcore.SelectionIntent{Host: host, Resource: hostcore.ResourceRef{Host: host, Owner: hostcore.SelectionOwner, Kind: hostcore.SelectionKind, ID: group},
		Provider: hostcore.Provider{ID: identity.ID, Revision: identity.Revision}, ExpectedRevision: generation, Member: member}
	result, err := hostcore.Select(ctx, intent, selectionExecutor{core: c})
	if errors.Is(err, hostcore.ErrSelectionRevision) {
		return result, ErrStaleGeneration
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	if errors.Is(err, hostcore.ErrSelectionScope) || errors.Is(err, hostcore.ErrSelectionProvider) || errors.Is(err, hostcore.ErrSelectionCapability) ||
		errors.Is(err, hostcore.ErrSelectionUnavailable) || errors.Is(err, hostcore.ErrSelectionVerification) || errors.Is(err, hostcore.ErrSelectionRecovery) || errors.Is(err, hostcore.ErrSelectionLease) {
		return result, ErrRuntimeAPIUnavailable
	}
	return result, err
}

func (c *Core) selectionIdentity() coreprovider.Identity {
	if c.selectionProvider != nil {
		return c.selectionProvider()
	}
	return coreprovider.Current()
}

type selectionExecutor struct{ core *Core }

func (e selectionExecutor) WithSelectionLease(ctx context.Context, intent hostcore.SelectionIntent, call func(context.Context, hostcore.SelectionLease) error) error {
	// Retain the established mutation serializer. Cancellation is checked
	// after admission and cannot interrupt another operation's active lease.
	e.core.mutation.Lock()
	defer e.core.mutation.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.core.withPrivateAPI(ctx, intent.ExpectedRevision, func(ctx context.Context, api *privateAPI) error {
		identity := e.core.selectionIdentity()
		if !coreprovider.SourceIdentityVerified(identity) {
			return ErrRuntimeAPIUnavailable
		}
		admission := hostcore.SelectionAdmission{Host: hostref.Local(), Resource: intent.Resource,
			Provider: hostcore.Provider{ID: identity.ID, Revision: identity.Revision}, Revision: api.generation, Capability: hostcore.Available}
		// The public entry point has already bound this resource to the local
		// CoreRuntime owner. Do not reconstruct authority from a raw RPC call.
		if admission.Resource.Host != hostref.Local() || admission.Resource.Owner != hostcore.SelectionOwner || admission.Resource.Kind != hostcore.SelectionKind {
			return hostcore.ErrSelectionScope
		}
		return call(ctx, privateSelectionLease{api: api, admission: admission})
	})
}

type privateSelectionLease struct {
	api       *privateAPI
	admission hostcore.SelectionAdmission
}

func (l privateSelectionLease) Admission() hostcore.SelectionAdmission { return l.admission }
func (l privateSelectionLease) ReadSelection(ctx context.Context) (hostcore.SelectionSnapshot, error) {
	groups, err := readRuntimeGroups(ctx, l.api)
	if err != nil {
		return hostcore.SelectionSnapshot{}, err
	}
	for _, group := range groups.Groups {
		if group.Tag == l.admission.Resource.ID {
			result := hostcore.SelectionSnapshot{Host: l.admission.Host, Resource: l.admission.Resource, Provider: l.admission.Provider,
				Revision: groups.Generation, Selectable: group.Selectable, Selected: group.Selected, Members: make([]string, 0, len(group.Items))}
			for _, item := range group.Items {
				result.Members = append(result.Members, item.Tag)
			}
			return result, nil
		}
	}
	return hostcore.SelectionSnapshot{}, ErrRuntimeAPIUnavailable
}
func (l privateSelectionLease) SelectMember(ctx context.Context, member string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := l.api.rpc.SelectOutbound(ctx, &daemon.SelectOutboundRequest{GroupTag: l.admission.Resource.ID, OutboundTag: member})
	if err != nil {
		return ErrRuntimeAPIUnavailable
	}
	return nil
}
