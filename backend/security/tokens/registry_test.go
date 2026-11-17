package tokens

import "testing"

func ResetAPITokenScopeProvidersForTest() {
	apiTokenScopeProviders.Lock()
	apiTokenScopeProviders.list = nil
	apiTokenScopeProviders.Unlock()
}

func TestAPITokenScopeProviderStaleCleanupPreservesNewRegistration(t *testing.T) {
	ResetAPITokenScopeProvidersForTest()
	t.Cleanup(ResetAPITokenScopeProvidersForTest)
	cleanupOld := RegisterAPITokenScopeProvider(func() []string { return []string{"old-scope"} })
	ResetAPITokenScopeProvidersForTest()
	cleanupNew := RegisterAPITokenScopeProvider(func() []string { return []string{"new-scope"} })
	t.Cleanup(cleanupNew)

	cleanupOld()
	if !ScopeAllowed("new-scope") {
		t.Fatal("stale cleanup removed the new token-scope provider")
	}
}
