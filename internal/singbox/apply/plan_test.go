package apply

import (
	"reflect"
	"testing"
)

func TestPlanReturnsCopiedSlices(t *testing.T) {
	plan := NewPlan("clients")
	plan.IncludeObjects("inbounds")
	plan.RequireCoreRestart()
	plan.MergeOutboundChange(&Change{
		ReloadIDs:  []uint{1},
		RemoveTags: []string{"old-out"},
	})

	objects := plan.Objects()
	objects[0] = "mutated"
	outboundIDs := plan.OutboundIDs()
	outboundIDs[0] = 99
	removed := plan.RemovedOutboundTags()
	removed[0] = "mutated"

	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"clients", "inbounds"}) {
		t.Fatalf("plan objects were mutated through returned slice: %#v", got)
	}
	if got := plan.OutboundIDs(); !reflect.DeepEqual(got, []uint{1}) {
		t.Fatalf("outbound ids were mutated through returned slice: %#v", got)
	}
	if got := plan.RemovedOutboundTags(); !reflect.DeepEqual(got, []string{"old-out"}) {
		t.Fatalf("removed outbound tags were mutated through returned slice: %#v", got)
	}
	if !plan.RequiresCoreRestart() {
		t.Fatal("expected core restart flag")
	}
}

func TestPlanMergesEntityCoreChanges(t *testing.T) {
	plan := NewPlan("outbounds")

	plan.MergeOutboundChange(&Change{
		ReloadIDs:  []uint{1, 2, 2, 0},
		RemoveTags: []string{"old-out", "old-out", ""},
	})
	plan.MergeEndpointChange(&Change{
		ReloadIDs:  []uint{3, 3, 0},
		RemoveTags: []string{"old-ep", "old-ep"},
	})
	plan.MergeInboundChange(&Change{
		ReloadIDs:         []uint{4, 4},
		RemoveTags:        []string{"old-in", "old-in"},
		CascadeServiceIDs: []uint{9, 9, 0},
	})
	plan.MergeServiceChange(&Change{
		ReloadIDs:  []uint{5, 9, 5},
		RemoveTags: []string{"old-service", "old-service"},
	})

	assertUintSlice(t, "outbound ids", plan.OutboundIDs(), []uint{1, 2})
	assertUintSlice(t, "endpoint ids", plan.EndpointIDs(), []uint{3})
	assertUintSlice(t, "inbound ids", plan.InboundIDs(), []uint{4})
	assertUintSlice(t, "service ids", plan.ServiceIDs(), []uint{9, 5})
	assertStringSlice(t, "removed outbound tags", plan.RemovedOutboundTags(), []string{"old-out"})
	assertStringSlice(t, "removed endpoint tags", plan.RemovedEndpointTags(), []string{"old-ep"})
	assertStringSlice(t, "removed inbound tags", plan.RemovedInboundTags(), []string{"old-in"})
	assertStringSlice(t, "removed service tags", plan.RemovedServiceTags(), []string{"old-service"})
	if !plan.HasObjectChanges() {
		t.Fatal("merged entity changes must mark the plan as having object changes")
	}
}

func TestPlanMergeRestartReason(t *testing.T) {
	plan := NewPlan("outbounds")

	plan.MergeOutboundChange(&Change{
		NeedsRestart:  true,
		RestartReason: "outbound is eagerly referenced",
	})
	plan.MergeServiceChange(&Change{NeedsRestart: true})

	if !plan.RequiresCoreRestart() {
		t.Fatal("expected merged restart requirement")
	}
	if plan.RestartReason() != "outbound is eagerly referenced" {
		t.Fatalf("empty restart reason should not replace existing reason, got %q", plan.RestartReason())
	}

	plan.MergeEndpointChange(&Change{
		NeedsRestart:  true,
		RestartReason: "endpoint detour changed",
	})
	if plan.RestartReason() != "endpoint detour changed" {
		t.Fatalf("non-empty restart reason should update reason, got %q", plan.RestartReason())
	}
}

func TestPlanNilChangesAreIgnored(t *testing.T) {
	plan := NewPlan("services")

	plan.MergeInboundChange(nil)
	plan.MergeOutboundChange(nil)
	plan.MergeEndpointChange(nil)
	plan.MergeServiceChange(nil)

	if plan.RequiresCoreRestart() {
		t.Fatal("nil entity changes must not require core restart")
	}
	if plan.HasObjectChanges() {
		t.Fatal("nil entity changes must not add object changes")
	}
}

func assertUintSlice(t *testing.T, name string, got []uint, expected []uint) {
	t.Helper()
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("%s mismatch: got=%v expected=%v", name, got, expected)
	}
}

func assertStringSlice(t *testing.T, name string, got []string, expected []string) {
	t.Helper()
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("%s mismatch: got=%v expected=%v", name, got, expected)
	}
}
