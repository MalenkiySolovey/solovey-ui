package service

import (
	"testing"

	"github.com/deposist/s-ui-x/core"
	"github.com/deposist/s-ui-x/database/model"
)

func TestStatsServiceSaveStatsWithEmptyStats(t *testing.T) {
	coreInstance := core.NewCore()
	if err := coreInstance.Start([]byte(`{"log":{"disabled":true},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Skipf("minimal core start unavailable for empty-stats regression: %v", err)
	}
	t.Cleanup(func() {
		_ = coreInstance.Stop()
	})

	onlineResourcesMu.Lock()
	onlineResources = &onlines{User: []string{"stale-user"}}
	onlineResourcesMu.Unlock()

	statsService := &StatsService{Runtime: NewRuntime(coreInstance)}
	if err := statsService.SaveStats(true); err != nil {
		t.Fatal(err)
	}
	current, err := statsService.GetOnlines()
	if err != nil {
		t.Fatal(err)
	}
	if len(current.User) != 0 || len(current.Inbound) != 0 || len(current.Outbound) != 0 {
		t.Fatalf("empty stats should clear online resources: %#v", current)
	}
}

func TestStatsServiceDownsampleStatsBucketsExtra(t *testing.T) {
	statsService := &StatsService{}
	input := []model.Stats{
		{DateTime: 100, Resource: "user", Tag: "alice", Direction: false, Traffic: 10},
		{DateTime: 101, Resource: "user", Tag: "alice", Direction: false, Traffic: 30},
		{DateTime: 102, Resource: "user", Tag: "alice", Direction: true, Traffic: 40},
		{DateTime: 110, Resource: "user", Tag: "alice", Direction: false, Traffic: 90},
		{DateTime: 111, Resource: "user", Tag: "alice", Direction: true, Traffic: 100},
		{DateTime: 112, Resource: "user", Tag: "alice", Direction: true, Traffic: 140},
	}

	got := statsService.downsampleStats(input, 4)
	if len(got) != 4 {
		t.Fatalf("expected 4 downsampled rows, got %d: %#v", len(got), got)
	}
	if got[0].Direction || got[0].Traffic != 20 {
		t.Fatalf("unexpected first bucket down row: %#v", got[0])
	}
	if !got[1].Direction || got[1].Traffic != 40 {
		t.Fatalf("unexpected first bucket up row: %#v", got[1])
	}
	if got[2].Direction || got[2].Traffic != 90 {
		t.Fatalf("unexpected second bucket down row: %#v", got[2])
	}
	if !got[3].Direction || got[3].Traffic != 120 {
		t.Fatalf("unexpected second bucket up row: %#v", got[3])
	}
	if got[0].Resource != "user" || got[0].Tag != "alice" {
		t.Fatalf("resource/tag not preserved: %#v", got[0])
	}
	if got[0].DateTime > got[2].DateTime {
		t.Fatalf("bucket order regressed: %#v", got)
	}
}
