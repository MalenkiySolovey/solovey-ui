package firewall

import (
	"context"
	"testing"
)

type countingSSHPostureSource struct{ calls int }

func (source *countingSSHPostureSource) WithCurrentPosture(ctx context.Context) context.Context {
	source.calls++
	return ctx
}

type unreadableBaselineStore struct{ BaselineStore }

func TestRuntimeWithoutSSHOwnerDoesNotReadDurablePolicy(t *testing.T) {
	service := NewBaselineService(unreadableBaselineStore{}, nil, struct{ NFTCodec }{}, unreadBaselineSources(t))
	if _, err := service.RuntimeManagement(t.Context()); err == nil {
		t.Fatal("missing SSH owner admitted runtime authority")
	}
}

func TestBaselineWithoutDurableOwnerDoesNotObserveSSH(t *testing.T) {
	source := &countingSSHPostureSource{}
	service := NewBaselineService(nil, source, struct{ NFTCodec }{}, unreadBaselineSources(t))
	if _, err := service.Snapshot(t.Context(), true, nil); err == nil {
		t.Fatal("missing durable owner admitted baseline observation")
	}
	if _, err := service.RuntimeManagement(t.Context()); err == nil {
		t.Fatal("missing durable owner admitted runtime observation")
	}
	if source.calls != 0 {
		t.Fatal("inactive baseline observed the SSH owner")
	}
}
