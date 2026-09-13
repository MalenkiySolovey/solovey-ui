package service

import (
	"sort"
	"sync"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

type PanelEventNotifier func(event string, fields map[string]string)

type PanelAuthenticationEventV1 struct {
	Event           string
	User            string
	SessionRevision string
	ClientIdentity  clientidentity.V1
}

type PanelAuthenticationEventNotifier func(PanelAuthenticationEventV1)

type registeredPanelAuthenticationEventNotifier struct {
	notifier PanelAuthenticationEventNotifier
	token    uint64
}

const maxPanelEventNotifiers = 128

type registeredPanelEventNotifier struct {
	notifier PanelEventNotifier
	token    uint64
}

var panelEventNotifiers = struct {
	sync.RWMutex
	entries   map[string]registeredPanelEventNotifier
	nextToken uint64
}{
	entries: map[string]registeredPanelEventNotifier{},
}

var panelAuthenticationEventNotifiers = struct {
	sync.RWMutex
	entries   map[string]registeredPanelAuthenticationEventNotifier
	nextToken uint64
}{entries: map[string]registeredPanelAuthenticationEventNotifier{}}

func RegisterPanelAuthenticationEventNotifier(name string, fn PanelAuthenticationEventNotifier) func() {
	if name == "" || fn == nil {
		panic("panel authentication event notifier name and callback are required")
	}
	panelAuthenticationEventNotifiers.Lock()
	if _, exists := panelAuthenticationEventNotifiers.entries[name]; exists {
		panelAuthenticationEventNotifiers.Unlock()
		panic("panel authentication event notifier already registered: " + name)
	}
	if len(panelAuthenticationEventNotifiers.entries) >= maxPanelEventNotifiers {
		panelAuthenticationEventNotifiers.Unlock()
		panic("panel authentication event notifier registry capacity exceeded")
	}
	panelAuthenticationEventNotifiers.nextToken++
	token := panelAuthenticationEventNotifiers.nextToken
	panelAuthenticationEventNotifiers.entries[name] = registeredPanelAuthenticationEventNotifier{notifier: fn, token: token}
	panelAuthenticationEventNotifiers.Unlock()
	return func() {
		panelAuthenticationEventNotifiers.Lock()
		if current, exists := panelAuthenticationEventNotifiers.entries[name]; exists && current.token == token {
			delete(panelAuthenticationEventNotifiers.entries, name)
		}
		panelAuthenticationEventNotifiers.Unlock()
	}
}

func NotifyPanelAuthenticationEvent(event PanelAuthenticationEventV1) {
	panelAuthenticationEventNotifiers.RLock()
	names := make([]string, 0, len(panelAuthenticationEventNotifiers.entries))
	for name := range panelAuthenticationEventNotifiers.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]PanelAuthenticationEventNotifier, 0, len(names))
	for _, name := range names {
		entries = append(entries, panelAuthenticationEventNotifiers.entries[name].notifier)
	}
	panelAuthenticationEventNotifiers.RUnlock()
	for _, notifier := range entries {
		func(fn PanelAuthenticationEventNotifier) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Warning("panel authentication event notifier panic: ", recovered)
				}
			}()
			fn(event)
		}(notifier)
	}
}

func RegisterPanelEventNotifier(name string, fn PanelEventNotifier) func() {
	if name == "" || fn == nil {
		panic("panel event notifier name and callback are required")
	}
	panelEventNotifiers.Lock()
	if _, exists := panelEventNotifiers.entries[name]; exists {
		panelEventNotifiers.Unlock()
		panic("panel event notifier already registered: " + name)
	}
	if len(panelEventNotifiers.entries) >= maxPanelEventNotifiers {
		panelEventNotifiers.Unlock()
		panic("panel event notifier registry capacity exceeded")
	}
	panelEventNotifiers.nextToken++
	token := panelEventNotifiers.nextToken
	panelEventNotifiers.entries[name] = registeredPanelEventNotifier{notifier: fn, token: token}
	panelEventNotifiers.Unlock()
	return func() {
		panelEventNotifiers.Lock()
		if current, ok := panelEventNotifiers.entries[name]; ok && current.token == token {
			delete(panelEventNotifiers.entries, name)
		}
		panelEventNotifiers.Unlock()
	}
}

func NotifyPanelEvent(event string, fields map[string]string) {
	for _, notifier := range panelEventNotifierEntries() {
		func(fn PanelEventNotifier) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Warning("panel event notifier panic: ", recovered)
				}
			}()
			fn(event, cloneStringMap(fields))
		}(notifier)
	}
}

func panelEventNotifierEntries() []PanelEventNotifier {
	panelEventNotifiers.RLock()
	defer panelEventNotifiers.RUnlock()
	names := make([]string, 0, len(panelEventNotifiers.entries))
	for name := range panelEventNotifiers.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]PanelEventNotifier, 0, len(names))
	for _, name := range names {
		entries = append(entries, panelEventNotifiers.entries[name].notifier)
	}
	return entries
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
