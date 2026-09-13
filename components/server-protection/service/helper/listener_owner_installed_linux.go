//go:build linux

package helper

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

// supervisorListenerOwnerExecutor is composition only. Installed backend
// adapters retain all supervisor-specific observation and proof semantics.
type supervisorListenerOwnerExecutor struct{}

var listenerOwnerProofAdapters []ListenerOwnerExecutor

func registerListenerOwnerProofAdapter(adapter ListenerOwnerExecutor) {
	listenerOwnerProofAdapters = append(listenerOwnerProofAdapters, adapter)
}

func (supervisorListenerOwnerExecutor) Detect(ctx context.Context) ListenerOwnerSupport {
	expected, err := deploymentidentity.LoadExpectedApplicationOwner()
	if err != nil {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_contract_unavailable", ObserverRevision: listenerOwnerObserverDigest()}
	}
	_, support, ok := selectedListenerOwnerProofAdapter(ctx, expected.ContractRevision)
	if !ok {
		return ListenerOwnerSupport{PlatformKnown: true, Linux: true, Reason: "listener_owner_contract_unavailable", ContractRevision: expected.ContractRevision, ObserverRevision: listenerOwnerObserverDigest()}
	}
	return support
}

func (supervisorListenerOwnerExecutor) Observe(ctx context.Context, request ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error) {
	expected, err := deploymentidentity.LoadExpectedApplicationOwner()
	if err != nil {
		result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}, ReasonCodes: []string{"listener_owner_contract_unavailable"}}
		sealListenerOwnerResult(result)
		return result, nil
	}
	if expected.ContractRevision != request.ExpectedOwnerContractRevision {
		result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}, ReasonCodes: []string{"listener_deployment_mismatch"}}
		sealListenerOwnerResult(result)
		return result, nil
	}
	adapter, _, ok := selectedListenerOwnerProofAdapter(ctx, expected.ContractRevision)
	if !ok {
		result := &ListenerOwnerObserveResult{Facts: []hostfacts.ListenerOwnerFactV1{}, ReasonCodes: []string{"listener_owner_contract_unavailable"}}
		sealListenerOwnerResult(result)
		return result, nil
	}
	return adapter.Observe(ctx, request)
}

func selectedListenerOwnerProofAdapter(ctx context.Context, contractRevision string) (ListenerOwnerExecutor, ListenerOwnerSupport, bool) {
	var selected ListenerOwnerExecutor
	var selectedSupport ListenerOwnerSupport
	matches := 0
	for _, adapter := range listenerOwnerProofAdapters {
		support := adapter.Detect(ctx)
		if support.ContractRevision == contractRevision {
			selected, selectedSupport, matches = adapter, support, matches+1
		}
	}
	return selected, selectedSupport, matches == 1
}
