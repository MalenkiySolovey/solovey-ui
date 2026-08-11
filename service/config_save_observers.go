package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

type ConfigSaveObserverContext struct {
	Service   *ConfigService
	Object    string
	Data      json.RawMessage
	LoginUser string
}

type ConfigSaveAfterCommit func()

type ConfigSaveObserver func(ConfigSaveObserverContext) (ConfigSaveAfterCommit, error)

const maxConfigSaveObservers = 128

type registeredConfigSaveObserver struct {
	observer ConfigSaveObserver
	token    uint64
}

var configSaveObservers = struct {
	sync.RWMutex
	entries   map[string]registeredConfigSaveObserver
	nextToken uint64
}{
	entries: map[string]registeredConfigSaveObserver{},
}

func RegisterConfigSaveObserver(name string, observer ConfigSaveObserver) func() {
	if name == "" {
		panic("config save observer name is required")
	}
	if observer == nil {
		panic(fmt.Errorf("config save observer %q is nil", name))
	}

	configSaveObservers.Lock()
	if _, exists := configSaveObservers.entries[name]; exists {
		configSaveObservers.Unlock()
		panic(fmt.Errorf("config save observer %q already registered", name))
	}
	if len(configSaveObservers.entries) >= maxConfigSaveObservers {
		configSaveObservers.Unlock()
		panic("config save observer registry capacity exceeded")
	}
	configSaveObservers.nextToken++
	token := configSaveObservers.nextToken
	configSaveObservers.entries[name] = registeredConfigSaveObserver{observer: observer, token: token}
	configSaveObservers.Unlock()

	return func() {
		configSaveObservers.Lock()
		if current, ok := configSaveObservers.entries[name]; ok && current.token == token {
			delete(configSaveObservers.entries, name)
		}
		configSaveObservers.Unlock()
	}
}

func resetConfigSaveObserversForTest() {
	configSaveObservers.Lock()
	configSaveObservers.entries = map[string]registeredConfigSaveObserver{}
	configSaveObservers.Unlock()
}

func (s *ConfigService) prepareConfigSaveObserverEffects(obj string, data json.RawMessage, loginUser string) ([]ConfigSaveAfterCommit, error) {
	observers := configSaveObserverSnapshot()
	if len(observers) == 0 {
		return nil, nil
	}

	ctx := ConfigSaveObserverContext{
		Service:   s,
		Object:    obj,
		Data:      data,
		LoginUser: loginUser,
	}
	effects := make([]ConfigSaveAfterCommit, 0, len(observers))
	for _, entry := range observers {
		effect, err := entry.observer(ctx)
		if err != nil {
			return nil, fmt.Errorf("config save observer %q failed: %w", entry.name, err)
		}
		if effect != nil {
			effects = append(effects, effect)
		}
	}
	return effects, nil
}

type configSaveObserverEntry struct {
	name     string
	observer ConfigSaveObserver
}

func configSaveObserverSnapshot() []configSaveObserverEntry {
	configSaveObservers.RLock()
	entries := make([]configSaveObserverEntry, 0, len(configSaveObservers.entries))
	for name, registered := range configSaveObservers.entries {
		entries = append(entries, configSaveObserverEntry{name: name, observer: registered.observer})
	}
	configSaveObservers.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
