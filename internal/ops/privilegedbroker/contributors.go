package privilegedbroker

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

const maxHandlerContributors = 64

type HandlerContributor func(*Registry) error

var handlerContributors = struct {
	sync.RWMutex
	values map[string]HandlerContributor
}{values: make(map[string]HandlerContributor)}

// RegisterHandlerContributor is the neutral composition seam for optional
// broker-owned transports. Semantic handlers remain in their component; the
// privileged executable resolves registered contributions without naming it.
func RegisterHandlerContributor(owner string, contributor HandlerContributor) {
	if !identifierPattern.MatchString(owner) || contributor == nil {
		panic("invalid privileged broker handler contributor")
	}
	handlerContributors.Lock()
	defer handlerContributors.Unlock()
	if _, exists := handlerContributors.values[owner]; exists {
		panic(fmt.Sprintf("privileged broker handler contributor %q is already registered", owner))
	}
	if len(handlerContributors.values) >= maxHandlerContributors {
		panic("too many privileged broker handler contributors")
	}
	handlerContributors.values[owner] = contributor
}

func RegisterContributedHandlers(registry *Registry) error {
	if registry == nil {
		return errors.New("privileged broker registry is required")
	}
	handlerContributors.RLock()
	owners := make([]string, 0, len(handlerContributors.values))
	contributors := make(map[string]HandlerContributor, len(handlerContributors.values))
	for owner, contributor := range handlerContributors.values {
		owners = append(owners, owner)
		contributors[owner] = contributor
	}
	handlerContributors.RUnlock()
	sort.Strings(owners)
	for _, owner := range owners {
		if err := contributors[owner](registry); err != nil {
			return fmt.Errorf("register privileged broker handlers for %s: %w", owner, err)
		}
	}
	return nil
}
