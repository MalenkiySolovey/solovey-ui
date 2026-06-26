package service

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"gorm.io/gorm"
)

type recordingConfigCoreObjectApplier struct {
	calls []string
	fail  string
}

func (r *recordingConfigCoreObjectApplier) record(call string) error {
	r.calls = append(r.calls, call)
	if r.fail == call {
		return errors.New(call + " failed")
	}
	return nil
}

func (r *recordingConfigCoreObjectApplier) RemoveOutbounds(tags []string) error {
	return r.record("remove outbounds:" + stringsForTest(tags))
}

func (r *recordingConfigCoreObjectApplier) RemoveEndpoints(tags []string) error {
	return r.record("remove endpoints:" + stringsForTest(tags))
}

func (r *recordingConfigCoreObjectApplier) RemoveInbounds(tags []string) error {
	return r.record("remove inbounds:" + stringsForTest(tags))
}

func (r *recordingConfigCoreObjectApplier) RemoveServices(tags []string) error {
	return r.record("remove services:" + stringsForTest(tags))
}

func (r *recordingConfigCoreObjectApplier) RestartOutbounds(_ *gorm.DB, ids []uint) error {
	return r.record("restart outbounds:" + uintsForTest(ids))
}

func (r *recordingConfigCoreObjectApplier) RestartEndpoints(_ *gorm.DB, ids []uint) error {
	return r.record("restart endpoints:" + uintsForTest(ids))
}

func (r *recordingConfigCoreObjectApplier) RestartInbounds(_ *gorm.DB, ids []uint) error {
	return r.record("restart inbounds:" + uintsForTest(ids))
}

func (r *recordingConfigCoreObjectApplier) RestartServices(_ *gorm.DB, ids []uint) error {
	return r.record("restart services:" + uintsForTest(ids))
}

func TestApplyObjectChangesLockedRemovesBeforeRestarting(t *testing.T) {
	plan := newConfigSavePlan(singboxapply.ObjectOutbounds.String())
	plan.MergeOutboundChange(&singboxapply.Change{ReloadIDs: []uint{11}, RemoveTags: []string{"old-out"}})
	plan.MergeEndpointChange(&singboxapply.Change{ReloadIDs: []uint{22}, RemoveTags: []string{"old-endpoint"}})
	plan.MergeInboundChange(&singboxapply.Change{ReloadIDs: []uint{33}, RemoveTags: []string{"old-in"}, CascadeServiceIDs: []uint{55}})
	plan.MergeServiceChange(&singboxapply.Change{ReloadIDs: []uint{44}, RemoveTags: []string{"old-service"}})

	recorder := &recordingConfigCoreObjectApplier{}
	service := &ConfigService{coreObjectApplier: recorder}
	if err := service.applyObjectChangesLocked(plan); err != nil {
		t.Fatalf("applyObjectChangesLocked returned error: %v", err)
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

func TestApplyObjectChangesLockedStopsOnRemovalError(t *testing.T) {
	plan := newConfigSavePlan(singboxapply.ObjectOutbounds.String())
	plan.MergeOutboundChange(&singboxapply.Change{ReloadIDs: []uint{11}, RemoveTags: []string{"old-out"}})
	plan.MergeEndpointChange(&singboxapply.Change{ReloadIDs: []uint{22}, RemoveTags: []string{"old-endpoint"}})

	recorder := &recordingConfigCoreObjectApplier{fail: "remove endpoints:old-endpoint"}
	service := &ConfigService{coreObjectApplier: recorder}
	if err := service.applyObjectChangesLocked(plan); err == nil {
		t.Fatal("applyObjectChangesLocked returned nil, want removal error")
	}

	want := []string{
		"remove outbounds:old-out",
		"remove endpoints:old-endpoint",
	}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls mismatch:\n got: %#v\nwant: %#v", recorder.calls, want)
	}
}

func stringsForTest(values []string) string {
	return strings.Join(values, ",")
}

func uintsForTest(values []uint) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatUint(uint64(value), 10))
	}
	return strings.Join(out, ",")
}
