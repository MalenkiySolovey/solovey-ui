package diagnostics

import "testing"

func TestLogCategoryStaleCleanupPreservesNewRegistration(t *testing.T) {
	oldCleanup := RegisterLogCategory(LogCategoryContribution{
		Category: "test-generation",
		Match:    func(string, string) bool { return false },
	})
	oldCleanup()
	newCleanup := RegisterLogCategory(LogCategoryContribution{
		Category: "test-generation",
		Hint:     "new",
		Match:    func(string, string) bool { return true },
	})
	t.Cleanup(newCleanup)

	oldCleanup()
	if !registeredLogCategory("test-generation") || registeredLogHint("test-generation") != "new" {
		t.Fatal("stale cleanup changed the current log-category registration")
	}
}
