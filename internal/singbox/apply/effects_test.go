package apply

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type recordingObjectApplier struct {
	calls []string
	fail  string
}

func (r *recordingObjectApplier) record(call string) error {
	r.calls = append(r.calls, call)
	if r.fail == call {
		return errors.New(call + " failed")
	}
	return nil
}

func (r *recordingObjectApplier) RemoveOutbounds(tags []string) error {
	return r.record("remove outbounds:" + strings.Join(tags, ","))
}

func (r *recordingObjectApplier) RemoveEndpoints(tags []string) error {
	return r.record("remove endpoints:" + strings.Join(tags, ","))
}

func (r *recordingObjectApplier) RemoveInbounds(tags []string) error {
	return r.record("remove inbounds:" + strings.Join(tags, ","))
}

func (r *recordingObjectApplier) RemoveServices(tags []string) error {
	return r.record("remove services:" + strings.Join(tags, ","))
}

func (r *recordingObjectApplier) RestartOutbounds(_ *gorm.DB, ids []uint) error {
	return r.record("restart outbounds:" + joinUint(ids))
}

func (r *recordingObjectApplier) RestartEndpoints(_ *gorm.DB, ids []uint) error {
	return r.record("restart endpoints:" + joinUint(ids))
}

func (r *recordingObjectApplier) RestartInbounds(_ *gorm.DB, ids []uint) error {
	return r.record("restart inbounds:" + joinUint(ids))
}

func (r *recordingObjectApplier) RestartServices(_ *gorm.DB, ids []uint) error {
	return r.record("restart services:" + joinUint(ids))
}

func TestExecuteObjectChangesRemovesBeforeRestarting(t *testing.T) {
	plan := NewPlan("outbounds")
	plan.MergeOutboundChange(&Change{ReloadIDs: []uint{11}, RemoveTags: []string{"old-out"}})
	plan.MergeEndpointChange(&Change{ReloadIDs: []uint{22}, RemoveTags: []string{"old-endpoint"}})
	plan.MergeInboundChange(&Change{ReloadIDs: []uint{33}, RemoveTags: []string{"old-in"}, CascadeServiceIDs: []uint{55}})
	plan.MergeServiceChange(&Change{ReloadIDs: []uint{44}, RemoveTags: []string{"old-service"}})

	recorder := &recordingObjectApplier{}
	if err := ExecuteObjectChanges(nil, plan, recorder); err != nil {
		t.Fatalf("ExecuteObjectChanges returned error: %v", err)
	}

	want := []string{
		"remove outbounds:old-out",
		"remove endpoints:old-endpoint",
		"remove inbounds:old-in",
		"remove services:old-service",
		"restart outbounds:11",
		"restart endpoints:22",
		"restart inbounds:33",
		"restart services:55,44",
	}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls mismatch:\n got: %#v\nwant: %#v", recorder.calls, want)
	}
}

func TestExecuteObjectChangesStopsOnRemovalError(t *testing.T) {
	plan := NewPlan("outbounds")
	plan.MergeOutboundChange(&Change{ReloadIDs: []uint{11}, RemoveTags: []string{"old-out"}})
	plan.MergeEndpointChange(&Change{ReloadIDs: []uint{22}, RemoveTags: []string{"old-endpoint"}})

	recorder := &recordingObjectApplier{fail: "remove endpoints:old-endpoint"}
	if err := ExecuteObjectChanges(nil, plan, recorder); err == nil {
		t.Fatal("ExecuteObjectChanges returned nil, want removal error")
	}

	want := []string{
		"remove outbounds:old-out",
		"remove endpoints:old-endpoint",
	}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls mismatch:\n got: %#v\nwant: %#v", recorder.calls, want)
	}
}

func joinUint(values []uint) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatUint(uint64(value), 10))
	}
	return strings.Join(out, ",")
}
