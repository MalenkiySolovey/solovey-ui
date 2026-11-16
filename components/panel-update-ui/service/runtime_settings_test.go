package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

type componentSettingsFixture func(string, bool, string, string) error

func (f componentSettingsFixture) SetComponentEnabled(id string, enabled bool, actor, hostname string) error {
	return f(id, enabled, actor, hostname)
}

func TestComponentSettingsCommandKeepsFailureAndCompensationWithOneOwner(t *testing.T) {
	const id = "test-component-settings"
	registerRuntimeTestComponent(id)
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Profile: "full", Binary: "full", Components: []installstate.InstalledComponent{{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	for _, failSave := range []bool{true, false} {
		t.Run(map[bool]string{true: "save rejection", false: "lifecycle compensation"}[failSave], func(t *testing.T) {
			var order []string
			writeFailure := errors.New("settings write rejected")
			lifecycleFailure := errors.New("start failed")
			manager := RuntimeManager{Settings: componentSettingsFixture(func(componentID string, enabled bool, actor, hostname string) error {
				if componentID != id || actor != "admin" || hostname != "panel" {
					t.Fatalf("settings authority input: %s %t %s %s", componentID, enabled, actor, hostname)
				}
				if enabled {
					order = append(order, "enable")
				} else {
					order = append(order, "restore")
				}
				if failSave {
					return writeFailure
				}
				return nil
			}), Reconcile: func(context.Context) error {
				order = append(order, "reconcile")
				if len(order) == 2 {
					return lifecycleFailure
				}
				return nil
			}}
			_, err := manager.Enable(OperationContext{RequestContext: t.Context(), Actor: "admin", Hostname: "panel"}, id)
			want := "enable,reconcile,restore,reconcile"
			if failSave {
				want = "enable"
				if !errors.Is(err, writeFailure) {
					t.Fatalf("write failure changed: %v", err)
				}
			} else if !errors.Is(err, lifecycleFailure) {
				t.Fatalf("lifecycle failure changed: %v", err)
			}
			if got := strings.Join(order, ","); got != want {
				t.Fatalf("settings/lifecycle order=%s want=%s", got, want)
			}
		})
	}
}

func TestComponentSettingsCommandRejectsTypedNilOwnerBeforeReconcile(t *testing.T) {
	const id = "test-component-settings-nil"
	registerRuntimeTestComponent(id)
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Profile: "full", Binary: "full", Components: []installstate.InstalledComponent{{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	var owner *coreservice.ConfigService
	manager := NewRuntimeManager(owner)
	manager.Reconcile = func(context.Context) error { t.Fatal("missing settings owner reached reconcile"); return nil }
	if _, err := manager.Enable(OperationContext{RequestContext: t.Context()}, id); err == nil {
		t.Fatal("typed nil settings owner admitted mutation")
	}
}
