package service

import (
	"reflect"
	"testing"
	"unsafe"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
)

type recordingConfigCoreLifecycle struct {
	calls []string
}

func (r *recordingConfigCoreLifecycle) startCoreLocked(force bool) error {
	if force {
		r.calls = append(r.calls, "start:force")
	} else {
		r.calls = append(r.calls, "start")
	}
	return nil
}

func (r *recordingConfigCoreLifecycle) restartCoreLocked() error {
	r.calls = append(r.calls, "restart")
	return nil
}

func TestApplyCoreSaveEffectLockedFallsBackToFullRestartAfterPartialReloadError(t *testing.T) {
	plan := newConfigSavePlan(singboxapply.ObjectOutbounds.String())
	plan.MergeOutboundChange(&singboxapply.Change{ReloadIDs: []uint{11}})

	lifecycle := &recordingConfigCoreLifecycle{}
	service := &ConfigService{
		Runtime:           NewRuntime(runningCoreForConfigSaveTest(t)),
		coreObjectApplier: &recordingConfigCoreObjectApplier{fail: "restart outbounds:11"},
		coreLifecycle:     lifecycle,
	}

	service.applyCoreSaveEffectLocked(plan)

	if !reflect.DeepEqual(lifecycle.calls, []string{"restart"}) {
		t.Fatalf("lifecycle calls = %#v, want full restart fallback", lifecycle.calls)
	}
}

func TestApplyCoreSaveEffectLockedStartsStoppedCoreForObjectChange(t *testing.T) {
	plan := newConfigSavePlan(singboxapply.ObjectOutbounds.String())
	plan.MergeOutboundChange(&singboxapply.Change{ReloadIDs: []uint{11}})

	lifecycle := &recordingConfigCoreLifecycle{}
	service := &ConfigService{
		Runtime:       NewRuntime(coreruntime.NewCore()),
		coreLifecycle: lifecycle,
	}

	service.applyCoreSaveEffectLocked(plan)

	if !reflect.DeepEqual(lifecycle.calls, []string{"start:force"}) {
		t.Fatalf("lifecycle calls = %#v, want forced start for stopped core", lifecycle.calls)
	}
}

func TestApplyCoreSaveEffectLockedUsesFullRestartForRestartPlan(t *testing.T) {
	plan := newConfigSavePlan(singboxapply.ObjectConfig.String())
	plan.RequireCoreRestart("config changed")

	lifecycle := &recordingConfigCoreLifecycle{}
	service := &ConfigService{
		Runtime:       NewRuntime(runningCoreForConfigSaveTest(t)),
		coreLifecycle: lifecycle,
	}

	service.applyCoreSaveEffectLocked(plan)

	if !reflect.DeepEqual(lifecycle.calls, []string{"restart"}) {
		t.Fatalf("lifecycle calls = %#v, want full restart for restart plan", lifecycle.calls)
	}
}

func runningCoreForConfigSaveTest(t *testing.T) *coreruntime.Core {
	t.Helper()
	coreInstance := coreruntime.NewCore()
	setUnexportedFieldForConfigSaveTest(reflect.ValueOf(coreInstance).Elem().FieldByName("isRunning"), reflect.ValueOf(true))
	return coreInstance
}

func setUnexportedFieldForConfigSaveTest(field reflect.Value, value reflect.Value) {
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(value)
}
