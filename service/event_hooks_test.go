package service

import (
	"fmt"
	"testing"
)

func TestPanelEventNotifierRejectsDuplicateName(t *testing.T) {
	cleanup := RegisterPanelEventNotifier("test.duplicate", func(string, map[string]string) {})
	t.Cleanup(cleanup)

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate notifier registration did not panic")
		}
	}()
	RegisterPanelEventNotifier("test.duplicate", func(string, map[string]string) {})
}

func TestPanelEventNotifierStaleCleanupPreservesNewRegistration(t *testing.T) {
	const name = "test.cleanup-generation"
	cleanupOld := RegisterPanelEventNotifier(name, func(string, map[string]string) {})
	cleanupOld()
	called := false
	cleanupNew := RegisterPanelEventNotifier(name, func(string, map[string]string) { called = true })
	t.Cleanup(cleanupNew)

	cleanupOld()
	NotifyPanelEvent("test", nil)
	if !called {
		t.Fatal("stale cleanup removed the new registration")
	}
}

func TestPanelEventNotifierRegistryBoundsCardinality(t *testing.T) {
	panelEventNotifiers.RLock()
	baseline := len(panelEventNotifiers.entries)
	panelEventNotifiers.RUnlock()

	cleanups := make([]func(), 0, maxPanelEventNotifiers-baseline)
	defer func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}()
	for index := baseline; index < maxPanelEventNotifiers; index++ {
		cleanups = append(cleanups, RegisterPanelEventNotifier(
			fmt.Sprintf("test.capacity.%03d", index),
			func(string, map[string]string) {},
		))
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registry accepted a notifier past its cardinality bound")
		}
	}()
	RegisterPanelEventNotifier("test.capacity.overflow", func(string, map[string]string) {})
}
