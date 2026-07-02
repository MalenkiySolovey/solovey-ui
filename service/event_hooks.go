package service

import (
	"sort"
	"sync"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

type PanelEventNotifier func(event string, fields map[string]string)

var panelEventNotifiers = struct {
	sync.RWMutex
	entries map[string]PanelEventNotifier
}{
	entries: map[string]PanelEventNotifier{},
}

func RegisterPanelEventNotifier(name string, fn PanelEventNotifier) func() {
	if name == "" || fn == nil {
		return func() {}
	}
	panelEventNotifiers.Lock()
	panelEventNotifiers.entries[name] = fn
	panelEventNotifiers.Unlock()
	return func() {
		panelEventNotifiers.Lock()
		delete(panelEventNotifiers.entries, name)
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

func ResetPanelEventNotifiersForTest() {
	panelEventNotifiers.Lock()
	panelEventNotifiers.entries = map[string]PanelEventNotifier{}
	panelEventNotifiers.Unlock()
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
		entries = append(entries, panelEventNotifiers.entries[name])
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
