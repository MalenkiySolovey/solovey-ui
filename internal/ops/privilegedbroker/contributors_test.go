package privilegedbroker

import (
	"errors"
	"testing"
)

func TestRegisterContributedHandlersIsDeterministicAndPropagatesOwnerError(t *testing.T) {
	handlerContributors.Lock()
	previous := handlerContributors.values
	handlerContributors.values = make(map[string]HandlerContributor)
	handlerContributors.Unlock()
	t.Cleanup(func() {
		handlerContributors.Lock()
		handlerContributors.values = previous
		handlerContributors.Unlock()
	})

	order := make([]string, 0, 2)
	RegisterHandlerContributor("z-owner", func(*Registry) error {
		order = append(order, "z-owner")
		return nil
	})
	wantErr := errors.New("owner failure")
	RegisterHandlerContributor("a-owner", func(*Registry) error {
		order = append(order, "a-owner")
		return wantErr
	})
	if err := RegisterContributedHandlers(NewRegistry()); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want owner failure", err)
	}
	if len(order) != 1 || order[0] != "a-owner" {
		t.Fatalf("contributors ran out of order: %v", order)
	}
}

func TestRegisterHandlerContributorRejectsDuplicateOwner(t *testing.T) {
	handlerContributors.Lock()
	previous := handlerContributors.values
	handlerContributors.values = make(map[string]HandlerContributor)
	handlerContributors.Unlock()
	t.Cleanup(func() {
		handlerContributors.Lock()
		handlerContributors.values = previous
		handlerContributors.Unlock()
	})

	RegisterHandlerContributor("owner", func(*Registry) error { return nil })
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate contributor did not panic")
		}
	}()
	RegisterHandlerContributor("owner", func(*Registry) error { return nil })
}
