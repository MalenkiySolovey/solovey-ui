package runtime

import (
	"errors"
	"testing"
)

func TestProviderFailureDoesNotPublishGeneration(t *testing.T) {
	core := NewCore()
	missing := []byte(`{"inbounds":[{"type":"http","tag":"in","listen":"127.0.0.1","listen_port":0,"tls":{"enabled":true,"certificate_provider":"missing"}}]}`)
	if err := core.Start(missing); err == nil {
		t.Fatal("missing provider reference accepted")
	}
	if core.instance != nil || core.isRunning || core.managerGeneration != 0 || core.inboundManager != nil || core.statsTracker != nil {
		t.Fatal("failed generation published")
	}
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	instance, generation := core.instance, core.managerGeneration
	if err := core.Start(missing); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("running replacement guard=%v", err)
	}
	if core.instance != instance || core.managerGeneration != generation || !core.isRunning {
		t.Fatal("failed replacement disturbed active generation")
	}
}
