package ipmonitor

import "testing"

func TestSecurityEventAuditHookHasSingleLifecycleOwner(t *testing.T) {
	stop, err := RegisterHooks(Hooks{SecurityEventAudit: func(string, string, map[string]any) {}})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}
	if _, err := RegisterHooks(Hooks{SecurityEventAudit: func(string, string, map[string]any) {}}); err == nil {
		t.Fatal("second audit authority was accepted")
	}
	stop()
	stop()
	if next, err := RegisterHooks(Hooks{SecurityEventAudit: func(string, string, map[string]any) {}}); err != nil {
		t.Fatalf("hook remained registered after cleanup: %v", err)
	} else {
		next()
	}
}
