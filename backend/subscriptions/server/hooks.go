package server

import (
	"errors"
	"sync"
)

// Hooks are composition-owned adapters from the subscription runtime to audit
// and settings owners. The runtime snapshots them per call and never imports
// the service layer.
type Hooks struct {
	ListenFallbackAudit func(component, requestedAddr, fallbackAddr string, bindErr error)
	EnumerationAudit    func(ip string, invalidLookups, windowMinutes int)
	RateLimitProvider   func() (int, error)
}

var registeredHooks = struct {
	sync.RWMutex
	next  uint64
	id    uint64
	hooks Hooks
}{}

// RegisterHooks installs one lifecycle-owned adapter set. A second authority
// is rejected until the original registration is released.
func RegisterHooks(hooks Hooks) (func(), error) {
	if hooks.ListenFallbackAudit == nil || hooks.EnumerationAudit == nil || hooks.RateLimitProvider == nil {
		return nil, errors.New("subscription hooks are incomplete")
	}
	registeredHooks.Lock()
	defer registeredHooks.Unlock()
	if registeredHooks.id != 0 {
		return nil, errors.New("subscription hooks already registered")
	}
	registeredHooks.next++
	id := registeredHooks.next
	registeredHooks.id = id
	registeredHooks.hooks = hooks
	var once sync.Once
	return func() {
		once.Do(func() {
			registeredHooks.Lock()
			if registeredHooks.id == id {
				registeredHooks.id = 0
				registeredHooks.hooks = Hooks{}
			}
			registeredHooks.Unlock()
		})
	}, nil
}

func currentHooks() Hooks {
	registeredHooks.RLock()
	defer registeredHooks.RUnlock()
	return registeredHooks.hooks
}
