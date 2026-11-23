package entityinbounds

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
)

func TestZapretTUNRequestsFullCoreRestart(t *testing.T) {
	for _, row := range []struct {
		kind, name  string
		wantRestart bool
	}{
		{"tun", "svzap-tun0", true},
		{"tun", "ordinary", false},
		{"mixed", "svzap-tun0", false},
	} {
		change := &singboxapply.Change{}
		applyManagedTUNChange(change, model.Inbound{Type: row.kind, Options: json.RawMessage(`{"interface_name":"` + row.name + `"}`)})
		if change.NeedsRestart != row.wantRestart {
			t.Fatalf("incorrect reload for %s/%s: %+v", row.kind, row.name, change)
		}
	}
}
