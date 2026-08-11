//go:build !linux

package helper

import "context"

type unavailableListenerOwnerExecutor struct{}

func newSystemListenerOwnerExecutor() ListenerOwnerExecutor {
	return unavailableListenerOwnerExecutor{}
}

func (unavailableListenerOwnerExecutor) Detect(context.Context) ListenerOwnerSupport {
	return ListenerOwnerSupport{PlatformKnown: true, Linux: false, Reason: "listener_owner_platform_unsupported", ObserverRevision: listenerOwnerObserverDigest()}
}

func (unavailableListenerOwnerExecutor) Observe(context.Context, ListenerOwnerObserveRequest) (*ListenerOwnerObserveResult, error) {
	result := &ListenerOwnerObserveResult{Facts: nil, ReasonCodes: []string{"listener_owner_platform_unsupported"}}
	sealListenerOwnerResult(result)
	return result, nil
}
