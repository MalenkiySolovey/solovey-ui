//go:build !minimal

package zapret

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestZapretViewDoesNotShowRetiredProfileMatches(t *testing.T) {
	request := zapretactivation.Request{OutboundRef: "a", StrategyRef: "operator-strategy", OutboundRevision: strings.Repeat("1", 64)}
	snapshot := service.ZapretCatalogSnapshot{
		References: []outbounds.ZapretReference{{OutboundRef: request.OutboundRef, StrategyRef: request.StrategyRef, Enabled: true}},
		Desired:    []zapretactivation.Request{request},
		Applied: []zapretactivation.AppliedBinding{{Request: request, StrategyRevision: strings.Repeat("2", 64), Queue: 401,
			NativeMatches: &zapretactivation.NativeMatches{Profiles: []string{"active-profile"}}}},
	}
	check := func(state string, matches []string) {
		t.Helper()
		view, err := makeCatalogView(snapshot)
		if err != nil || len(view.References) != 1 {
			t.Fatalf("bad view: %+v, %v", view, err)
		}
		ref := view.References[0]
		if ref.State != state || matches == nil && ref.NativeMatches != nil ||
			matches != nil && (ref.NativeMatches == nil || !slices.Equal(ref.NativeMatches.Profiles, matches)) {
			t.Fatalf("wrong %s generation evidence: %+v", state, ref)
		}
	}
	check("applied", []string{"active-profile"})
	snapshot.Desired[0].OutboundRevision = strings.Repeat("3", 64)
	check("pending", nil)
	snapshot.Applied = nil
	check("blocked", nil)
	snapshot.References[0].Enabled = false
	check("off", nil)
}

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
	apply(catalogChange{Action: "delete", ID: id}, false, "referenced by outbound")
}

func TestPresetCatalogSaveFencesAndRevalidatesReview(t *testing.T) {
	catalog, err := readCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(preset strategy.PresetInput) catalogChange {
		t.Helper()
		change := catalogChange{Action: "import-preset", ExpectedRevision: catalog.Revision(), preset: &preset}
		if err := change.prepare(); err != nil {
			t.Fatal(err)
		}
		if !zapretactivation.ManagedStrategyRef(change.ID) {
			t.Fatalf("not a managed ID: %q", change.ID)
		}
		return change
	}
	valid := prepare(strategy.PresetInput{Format: "native", Content: []byte("--filter-tcp=443\n--new=udp --filter-udp=443\n")})
	raw, restart, err := valid.mutate(nil, nil)
	if err != nil || restart {
		t.Fatalf("import of an unused strategy should not restart Core: %t, %v", restart, err)
	}
	after, err := readCatalog(raw)
	if err != nil || len(after.Records[valid.ID].Definition.Profiles) != 2 {
		t.Fatalf("catalog does not contain the reviewed profiles: %v", err)
	}
	refs := []outbounds.ZapretReference{{OutboundRef: "proxy", StrategyRef: valid.ID, Enabled: true}}
	invalid := prepare(strategy.PresetInput{Format: "native", Content: []byte("--filter-tcp=443\n--lua-init=script.lua\n")})
	invalid.ExpectedRevision = after.Revision()
	for _, test := range []struct {
		change catalogChange
		want   string
	}{{valid, "catalog changed"}, {invalid, "Lua loading"}} {
		next, restart, err := test.change.mutate(raw, refs)
		if next != nil || restart || err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("invalid save returned a mutation or unexpected error: %s, %t, %v", next, restart, err)
		}
	}
}

func TestLuaDraftCatalogChangesDoNotRebind(t *testing.T) {
	const raw = "--filter-tcp=443\n"
	strategyID := zapretactivation.ManagedStrategyPrefix + strings.Repeat("2", 32)
	start, err := readCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := start.SealDraft(strategy.Draft{ID: strategyID, Name: "Selected", Raw: new(raw)})
	if err != nil {
		t.Fatal(err)
	}
	start.Records[strategyID] = record
	stored, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	refs := []outbounds.ZapretReference{{OutboundRef: "proxy", StrategyRef: strategyID, Enabled: true}}
	draft := &strategy.LuaDraft{Origin: "external", SourceRef: "Unknown", Source: "-- source only\r\n"}
	create := catalogChange{Action: "create-lua-draft", ID: "draft", ExpectedRevision: start.Revision(), luaDraft: draft}
	if err := create.prepare(); err != nil {
		t.Fatal(err)
	}
	after, restart, err := create.mutate(stored, refs)
	if err != nil || restart {
		t.Fatalf("saving Lua should only change storage: %t, %v", restart, err)
	}
	parsed, err := readCatalog(after)
	if err != nil || parsed.Records[strategyID].Definition.Revision != record.Definition.Revision || parsed.LuaDrafts["draft"] != *draft {
		t.Fatalf("draft or selected definition did not round trip: %v", err)
	}
	view, err := makeCatalogView(service.ZapretCatalogSnapshot{Catalog: after, References: refs})
	if err != nil || len(view.Catalog.LuaDrafts) != 0 || view.Revision != parsed.Revision() {
		t.Fatalf("ordinary Strategy view disclosed Lua source or drifted from the write revision: %v", err)
	}
	for _, tc := range []struct {
		action, revision, message string
	}{
		{"edit-lua-draft", start.Revision(), "catalog changed"},
		{"create-lua-draft", parsed.Revision(), "already in use"},
	} {
		change := catalogChange{Action: tc.action, ID: create.ID, ExpectedRevision: tc.revision, luaDraft: draft}
		next, restart, err := change.mutate(after, refs)
		if next != nil || restart || err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("expected %q and no mutation: %t, %v", tc.message, restart, err)
		}
	}
}

func TestZapretCatalogBlobTransactions(t *testing.T) {
	var raw json.RawMessage
	var refs []outbounds.ZapretReference
	apply := func(change catalogChange, rebind bool, wantError string) string {
		t.Helper()
		before, err := readCatalog(raw)
		if err != nil {
			t.Fatal(err)
		}
		change.ExpectedRevision = before.Revision()
		if err := change.prepare(); err != nil {
			t.Fatal(err)
		}
		next, restart, err := change.mutate(raw, refs)
		if wantError != "" {
			if err == nil || !strings.Contains(err.Error(), wantError) || next != nil || restart {
				t.Fatalf("%s did not reject the change atomically: restart=%t err=%v", change.Action, restart, err)
			}
			return ""
		}
		if err != nil || restart != rebind {
			t.Fatalf("%s: restart=%t, want %t: %v", change.Action, restart, rebind, err)
		}
		raw = next
		return change.ID
	}
	data := "405549"
	apply(catalogChange{Action: "create-blob", ID: "decoy", Blob: &data}, false, "")
	draft := strategy.Draft{Name: "QUIC profile", Profiles: []strategy.Profile{{ID: "quic", Protocol: "udp", Ports: []uint16{443},
		Payload: strategy.QUICInitial, QUICRepeats: 1, QUICBlobRef: "decoy"}}}
	id := apply(catalogChange{Action: "create", Draft: &draft}, false, "")
	refs = []outbounds.ZapretReference{{OutboundRef: "chosen", StrategyRef: id, Enabled: true}}
	apply(catalogChange{Action: "delete-blob", ID: "decoy"}, false, "is used by strategy")
	apply(catalogChange{Action: "edit-blob", ID: "decoy", Blob: &data}, false, "")
	apply(catalogChange{Action: "create-blob", ID: "unused", Blob: &data}, false, "")
	data = "405550"
	apply(catalogChange{Action: "edit-blob", ID: "unused", Blob: &data}, false, "")
	apply(catalogChange{Action: "edit-blob", ID: "decoy", Blob: &data}, true, "")
	after, err := readCatalog(raw)
	if err != nil || after.Records[id].Definition.BlobData["decoy"] != data {
		t.Fatalf("rebind did not project the resealed asset: %v", err)
	}
	refs[0].Enabled = false
	data = "405551"
	apply(catalogChange{Action: "edit-blob", ID: "decoy", Blob: &data}, false, "")
}
