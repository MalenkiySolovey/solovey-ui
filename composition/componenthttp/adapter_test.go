package componenthttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/componentroutes"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
)

func TestRuntimeInventoryWireParity(t *testing.T) {
	observed := time.Date(2026, 11, 17, 0, 1, 2, 345, time.UTC)
	for _, components := range [][]state.Component{nil, {}, {{ID: "telegram", Name: "Telegram", Version: "1", Since: "2026", Delivery: manifest.DeliveryInProcess, DefaultEnabled: true, TokenScopes: []string{"telegram"}, Installed: true, Enabled: true, Active: true, AvailableInBinary: true, ObservedLifecycle: state.LifecycleObservation{Phase: state.LifecycleStartFailed, InstanceRetention: state.InstanceUnknown, Source: "componenthost.supervisor", ObservedAt: &observed, ReasonCode: "START_FAILED"}}, {ID: "absent", Installed: true, ObservedLifecycle: state.LifecycleObservation{Phase: state.LifecycleBinaryAbsent}}}} {
		before, err := json.Marshal(struct {
			BinaryProfile string            `json:"binaryProfile"`
			Components    []state.Component `json:"components"`
		}{componentprofile.Binary, components})
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(projectInventory(components))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("installed-only inventory changed: before=%s after=%s", before, after)
		}
	}
}

func TestMissingProductAdapterRejectsEveryCapability(t *testing.T) {
	var adapter *Adapter
	if _, err := adapter.Routes(componentroutes.Ports{}); !errors.Is(err, componentroutes.ErrUnavailable) {
		t.Fatal(err)
	}
	if active, err := adapter.IsActive("telegram"); active || !errors.Is(err, componentroutes.ErrUnavailable) {
		t.Fatal(active, err)
	}
	if adapter.DeclaresAction("panel-update-ui", "component.remove") {
		t.Fatal("missing adapter declared an action")
	}
	if _, err := adapter.Inventory(); !errors.Is(err, componentroutes.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := adapter.Owners(); !errors.Is(err, componentroutes.ErrUnavailable) {
		t.Fatal(err)
	}
}
