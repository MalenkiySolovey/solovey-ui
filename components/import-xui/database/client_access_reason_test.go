//go:build !minimal

package importxui

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestImportedDisabledClientCannotAcquireQuotaResetAuthority(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		agg, err := newClientAggregate("import-access-fixture")
		if err != nil {
			t.Fatal(err)
		}
		agg.Enable, agg.Volume, agg.Up = enabled, 100, 101
		client, err := agg.toModel()
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if !enabled {
			want = model.ClientDisabledLegacy
		}
		if client.Enable != enabled || client.AccessDisableReason != want {
			t.Fatalf("import inferred quota authority from usage: %#v", client)
		}
	}
}
