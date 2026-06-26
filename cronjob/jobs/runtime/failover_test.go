package runtime

import (
	"context"
	"testing"
	"time"

	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service/failover"
)

func newTestFailoverJob(now func() time.Time, probe func(string) bool, active *string, switches *[]string) *FailoverJob {
	return &FailoverJob{
		states: make(map[string]*failoverGroupState),
		now:    now,
		probe:  func(_ context.Context, tag, _ string) bool { return probe(tag) },
		activeMember: func(string) (string, bool) {
			return *active, true
		},
		switchMember: func(_, target string) error {
			*switches = append(*switches, target)
			*active = target
			return nil
		},
	}
}

func TestFailoverJobFailoverThenHysteresisFailback(t *testing.T) {
	currentTime := time.Unix(1000, 0)
	active := "a"
	var switches []string
	health := map[string]bool{"a": true, "b": true}
	job := newTestFailoverJob(func() time.Time { return currentTime }, func(tag string) bool { return health[tag] }, &active, &switches)
	group := entityoutbounds.FailoverGroup{Tag: "g", Members: []string{"a", "b"}, ProbeTarget: "https://example.com", Interval: 30 * time.Second, Hysteresis: 2, Enabled: true}
	tick := func() {
		job.runGroup(nil, group, "")
		currentTime = currentTime.Add(group.Interval)
	}
	tick()
	health["a"] = false
	tick()
	health["a"] = true
	tick()
	tick()
	if len(switches) != 2 || switches[0] != "b" || switches[1] != "a" {
		t.Fatalf("switches = %v, want [b a]", switches)
	}
}

func TestFailoverJobAllDownUsesDirect(t *testing.T) {
	now := time.Unix(1000, 0)
	active := "a"
	var switches []string
	job := newTestFailoverJob(func() time.Time { return now }, func(string) bool { return false }, &active, &switches)
	group := entityoutbounds.FailoverGroup{Tag: "g", Members: []string{"a", "b"}, ProbeTarget: "https://example.com", Interval: 30 * time.Second, Hysteresis: 2, Enabled: true}
	job.runGroup(nil, group, "direct")
	if len(switches) != 1 || switches[0] != "direct" {
		t.Fatalf("switches = %v, want [direct]", switches)
	}
}

func TestFailoverJobDisabledGroupIsSkipped(t *testing.T) {
	now := time.Unix(1000, 0)
	active := "a"
	var switches []string
	job := newTestFailoverJob(func() time.Time { return now }, func(string) bool { return false }, &active, &switches)
	group := entityoutbounds.FailoverGroup{Tag: "g", Members: []string{"a"}, ProbeTarget: "https://example.com", Interval: 30 * time.Second, Hysteresis: 2}
	job.runGroup(nil, group, "direct")
	if len(switches) != 0 {
		t.Fatalf("disabled group switched: %v", switches)
	}
}

func TestFailoverJobPublishesLiveStatus(t *testing.T) {
	now := time.Unix(1000, 0)
	active := "a"
	var switches []string
	job := newTestFailoverJob(func() time.Time { return now }, func(tag string) bool { return tag == "b" }, &active, &switches)
	group := entityoutbounds.FailoverGroup{Tag: "g", Members: []string{"a", "b"}, ProbeTarget: "https://example.com", Interval: 30 * time.Second, Hysteresis: 1, Enabled: true}
	events := make(chan realtime.Event, 1)
	unregister := realtime.Register(&realtime.ClientHandle{
		User:   "test",
		IP:     "127.0.0.1",
		Scope:  realtime.ScopeRead,
		SendCh: events,
	})
	defer unregister()

	job.runGroup(nil, group, "")

	select {
	case event := <-events:
		if event.Type != realtime.TopicFailoverStatus {
			t.Fatalf("event.Type = %s, want %s", event.Type, realtime.TopicFailoverStatus)
		}
		status, ok := event.Payload.(failover.StatusEntry)
		if !ok {
			t.Fatalf("payload type = %T, want failover.StatusEntry", event.Payload)
		}
		if status.Tag != "g" || status.Active != "b" || status.AllDown {
			t.Fatalf("status = %#v", status)
		}
		if len(status.Members) != 2 || status.Members[0].Healthy || !status.Members[1].Healthy {
			t.Fatalf("members = %#v", status.Members)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failover live status event")
	}
}

func TestFailoverJobAllDownEdgeOnlyOnce(t *testing.T) {
	job := NewFailoverJob()
	if !job.recordAllDownEdge("g", true) {
		t.Fatal("first all-down transition should alert")
	}
	if job.recordAllDownEdge("g", true) {
		t.Fatal("steady all-down state should not alert again")
	}
	if job.recordAllDownEdge("g", false) {
		t.Fatal("recovery transition should not alert")
	}
	if !job.recordAllDownEdge("g", true) {
		t.Fatal("second down edge after recovery should alert")
	}
}
