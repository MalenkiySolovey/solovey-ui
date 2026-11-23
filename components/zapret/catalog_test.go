//go:build !minimal

package zapret

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
)

func TestZapretCatalogNamedListTransactions(t *testing.T) {
	var raw json.RawMessage
	var refs []outbounds.ZapretReference
	apply := func(change catalogChange, wantRestart bool, wantError string) string {
		t.Helper()
		catalog, err := readCatalog(raw)
		if err != nil {
			t.Fatal(err)
		}
		if change.ExpectedRevision == "" {
			change.ExpectedRevision = catalog.Revision()
		}
		if err := change.prepare(); err != nil {
			t.Fatal(err)
		}
		updated, restart, err := change.mutate(raw, refs)
		if wantError != "" {
			if err == nil || !strings.Contains(err.Error(), wantError) || updated != nil || restart {
				t.Fatalf("%s: wanted untouched catalog (%s), got %s, %t, %v", change.Action, wantError, updated, restart, err)
			}
			return ""
		}
		if err != nil || restart != wantRestart {
			t.Fatalf("%s: restart=%t, wanted %t: %v", change.Action, restart, wantRestart, err)
		}
		raw = updated
		return change.ID
	}
	hosts := strategy.NamedList{Kind: strategy.HostListKind, Entries: []string{"service.example"}}
	apply(catalogChange{Action: "create-list", ID: "service", List: &hosts}, false, "")
	apply(catalogChange{Action: "create-list", ID: "service", List: &hosts}, false, "already in use")
	first, err := readCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	draft := strategy.Draft{Name: "Native list consumer", Profiles: []strategy.Profile{{ID: "p1", Protocol: "tcp", Ports: []uint16{443}}},
		ListRefs: strategy.ListRefs{"p1": {HostList: "service"}}}
	id := apply(catalogChange{Action: "create", Draft: &draft}, false, "")
	refs = []outbounds.ZapretReference{{OutboundRef: "direct", StrategyRef: id, Enabled: true}}
	apply(catalogChange{Action: "delete-list", ID: "service"}, false, "is used by strategy")
	apply(catalogChange{Action: "edit-list", ID: "service", List: &hosts, ExpectedRevision: first.Revision()}, false, "catalog changed")
	apply(catalogChange{Action: "edit-list", ID: "service", List: &strategy.NamedList{Kind: strategy.HostListKind, Entries: []string{"SERVICE.EXAMPLE"}}}, false, "")
	apply(catalogChange{Action: "create-list", ID: "unrelated", List: &hosts}, false, "")
	before, err := readCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := before.Export(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfer.Lists) != 1 || !slices.Equal(transfer.Lists["service"].Entries, hosts.Entries) || strings.Contains(transfer.Strategy.Raw, "service.example") {
		t.Fatalf("export flattened or omitted native list references: %#v", transfer)
	}
	newHosts := strategy.NamedList{Kind: strategy.HostListKind, Entries: []string{"new.example"}}
	apply(catalogChange{Action: "edit-list", ID: "service", List: &newHosts}, true, "")
	after, err := readCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Records[id]; got.Definition.Revision == before.Records[id].Definition.Revision ||
		!slices.Equal(got.Definition.Profiles[0].Lists.HostList, newHosts.Entries) || got.Raw != before.Records[id].Raw {
		t.Fatalf("reference was not resealed independently of raw source: %#v", got)
	}
	apply(catalogChange{Action: "import", Transfer: &transfer}, false, "conflicts with an existing asset")
	refs[0].Enabled = false
	apply(catalogChange{Action: "edit-list", ID: "service", List: &hosts}, false, "")
	apply(catalogChange{Action: "import", Transfer: &transfer}, false, "")
	apply(catalogChange{Action: "delete", ID: id}, false, "referenced by Direct")
}
