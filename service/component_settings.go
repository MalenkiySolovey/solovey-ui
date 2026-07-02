package service

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
)

var componentSettingsReconciler = struct {
	sync.RWMutex
	fn func(context.Context) error
}{}

var componentMigrator = struct {
	sync.RWMutex
	fn func(context.Context) error
}{}

var componentDataDropper = struct {
	sync.RWMutex
	fn func(context.Context, string) error
}{}

func RegisterComponentSettingsReconciler(fn func(context.Context) error) {
	componentSettingsReconciler.Lock()
	defer componentSettingsReconciler.Unlock()
	componentSettingsReconciler.fn = fn
}

func RegisterComponentMigrator(fn func(context.Context) error) {
	componentMigrator.Lock()
	defer componentMigrator.Unlock()
	componentMigrator.fn = fn
}

func RegisterComponentDataDropper(fn func(context.Context, string) error) {
	componentDataDropper.Lock()
	defer componentDataDropper.Unlock()
	componentDataDropper.fn = fn
}

func reconcileComponentSettings(ctx context.Context) error {
	return ReconcileComponents(ctx)
}

func ReconcileComponents(ctx context.Context) error {
	componentSettingsReconciler.RLock()
	fn := componentSettingsReconciler.fn
	componentSettingsReconciler.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func MigrateComponents(ctx context.Context) error {
	componentMigrator.RLock()
	fn := componentMigrator.fn
	componentMigrator.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func DropComponentData(ctx context.Context, id string) error {
	componentDataDropper.RLock()
	fn := componentDataDropper.fn
	componentDataDropper.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, id)
}

func componentEnabledSettingsTouched(obj string, data json.RawMessage) (bool, error) {
	if obj != "settings" {
		return false, nil
	}
	settings, err := settingsDecodeSaveData(data)
	if err != nil {
		return false, err
	}
	for key := range settings {
		if enabledstate.IsSettingKey(key) {
			return true, nil
		}
	}
	return false, nil
}

func settingsDecodeSaveData(data json.RawMessage) (map[string]string, error) {
	var settings map[string]string
	err := json.Unmarshal(data, &settings)
	return settings, err
}
