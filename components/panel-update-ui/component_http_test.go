//go:build !minimal

package panelupdateui

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestComponentHTTPProjectionPreservesCatalogWireAndEmptySlices(t *testing.T) {
	at := time.Unix(123, 0).UTC()
	status := panelupdateservice.ComponentStatus{ID: "telegram", Name: "Telegram", Version: "1", LatestVersion: "2", Since: "2026.1", RequiredPanel: "2026.1",
		Delivery: manifest.DeliveryInProcess, DefaultEnabled: true, TokenScopes: []string{"read"}, AvailableInBinary: true, Compatible: true,
		Locked: true, LockedReason: "retained", Installable: true, Removable: true, Installed: true, Enabled: true, Active: true,
		ObservedLifecycle: state.LifecycleObservation{Phase: "STARTED", InstanceRetention: "RETAINED", Source: "componenthost.supervisor", ObservedAt: &at, ReasonCode: "START_RETURNED"},
		Group:             panelupdateservice.GroupInstalled, UnavailableReason: "fixture"}
	for _, statuses := range [][]panelupdateservice.ComponentStatus{nil, {}, {status}} {
		before, err := json.Marshal(statuses)
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(componentStatusesHTTP(statuses))
		if err != nil {
			t.Fatal(err)
		}
		var original, projected any
		if err := json.Unmarshal(before, &original); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(after, &projected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, projected) {
			t.Fatalf("catalog projection changed: before=%s after=%s", before, after)
		}
	}
}
