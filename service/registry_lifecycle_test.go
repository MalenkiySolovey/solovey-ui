package service

import (
	"testing"

	"gorm.io/gorm"
)

func TestConfigSaveObserverStaleCleanupPreservesNewRegistration(t *testing.T) {
	resetConfigSaveObserversForTest()
	t.Cleanup(resetConfigSaveObserversForTest)
	cleanupOld := RegisterConfigSaveObserver("test.generation", func(ConfigSaveObserverContext) (ConfigSaveAfterCommit, error) {
		return nil, nil
	})
	resetConfigSaveObserversForTest()
	cleanupNew := RegisterConfigSaveObserver("test.generation", func(ConfigSaveObserverContext) (ConfigSaveAfterCommit, error) {
		return func() {}, nil
	})
	t.Cleanup(cleanupNew)

	cleanupOld()
	entries := configSaveObserverSnapshot()
	if len(entries) != 1 || entries[0].name != "test.generation" {
		t.Fatalf("stale cleanup changed current observers: %#v", entries)
	}
}

func TestOutboundSaveHookStaleCleanupPreservesNewRegistration(t *testing.T) {
	ResetOutboundSaveHooksForTest()
	t.Cleanup(ResetOutboundSaveHooksForTest)
	cleanupOld := RegisterOutboundSaveHook("test.generation", func(*gorm.DB) error { return nil })
	ResetOutboundSaveHooksForTest()
	cleanupNew := RegisterOutboundSaveHook("test.generation", func(*gorm.DB) error { return nil })
	t.Cleanup(cleanupNew)

	cleanupOld()
	entries := outboundSaveHookSnapshot()
	if len(entries) != 1 || entries[0].name != "test.generation" {
		t.Fatalf("stale cleanup changed current hooks: %#v", entries)
	}
}

func TestAPITokenScopeProviderStaleCleanupPreservesNewRegistration(t *testing.T) {
	ResetAPITokenScopeProvidersForTest()
	t.Cleanup(ResetAPITokenScopeProvidersForTest)
	cleanupOld := RegisterAPITokenScopeProvider(func() []string { return []string{"old-scope"} })
	ResetAPITokenScopeProvidersForTest()
	cleanupNew := RegisterAPITokenScopeProvider(func() []string { return []string{"new-scope"} })
	t.Cleanup(cleanupNew)

	cleanupOld()
	if !apiTokenScopeAllowed("new-scope") {
		t.Fatal("stale cleanup removed the new token-scope provider")
	}
}
