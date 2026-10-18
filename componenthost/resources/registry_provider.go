package resources

import (
	"context"
	"errors"
	"strings"
	"time"
)

var errProviderCallUnavailable = errors.New("resource_provider_call_unavailable")

// Capacity is released on actual return, not on timeout of an uncooperative
// provider. Repeated reads cannot accumulate an unbounded set of workers.
var resourceProviderCalls = make(chan struct{}, maxContributors)

type providerIdentity interface {
	ProviderID() string
}

type identifiedProvider[T any] struct {
	id       string
	provider T
}

func stableProviderID(provider providerIdentity, valid func(string, int) bool) (id string, ok bool) {
	if provider == nil {
		return "", false
	}
	defer func() {
		if recover() != nil {
			id, ok = "", false
		}
	}()
	id = strings.TrimSpace(provider.ProviderID())
	return id, valid(id, 128)
}

func callResourceProvider[T any](ctx context.Context, timeout time.Duration, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if callCtx.Err() != nil || call == nil {
		return zero, errProviderCallUnavailable
	}
	select {
	case resourceProviderCalls <- struct{}{}:
	default:
		return zero, errProviderCallUnavailable
	}
	type response struct {
		value T
		err   error
	}
	result := make(chan response, 1)
	go func() {
		defer func() { <-resourceProviderCalls }()
		value := response{}
		defer func() {
			if recover() != nil {
				value = response{err: errProviderCallUnavailable}
			}
			result <- value
		}()
		value.value, value.err = call(callCtx)
	}()
	select {
	case value := <-result:
		if callCtx.Err() != nil {
			return zero, errProviderCallUnavailable
		}
		return value.value, value.err
	case <-callCtx.Done():
		return zero, errProviderCallUnavailable
	}
}
