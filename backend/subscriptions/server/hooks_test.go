package server

import "testing"

func completeHooks() Hooks {
	return Hooks{
		ListenFallbackAudit: func(string, string, string, error) {},
		EnumerationAudit:    func(string, int, int) {},
		RateLimitProvider:   func() (int, error) { return 10, nil },
	}
}

func TestHooksRequireOneCompleteLifecycleOwner(t *testing.T) {
	if _, err := RegisterHooks(Hooks{}); err == nil {
		t.Fatal("incomplete hooks were accepted")
	}
	stop, err := RegisterHooks(completeHooks())
	if err != nil {
		t.Fatalf("register hooks: %v", err)
	}
	if _, err := RegisterHooks(completeHooks()); err == nil {
		t.Fatal("second hook authority was accepted")
	}
	stop()
	stop()
	if next, err := RegisterHooks(completeHooks()); err != nil {
		t.Fatalf("hooks remained registered after cleanup: %v", err)
	} else {
		next()
	}
}
