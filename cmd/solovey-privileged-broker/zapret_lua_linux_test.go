//go:build linux && !minimal

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func TestResolveZapretBatchBindsManagedLuaToExactSource(t *testing.T) {
	const id = "ui-zapret-controlled"
	const native = "--filter-tcp=18080 --filter-l7=http --payload=http_req --lua-desync=observe"
	const scriptID = "example"
	script := strategy.LuaDraft{Origin: "external", SourceRef: "test fixture", Source: "function observe() return VERDICT_PASS end"}
	catalog := strategy.Catalog{LuaDrafts: map[string]strategy.LuaDraft{scriptID: script}}
	record, err := catalog.SealDraft(strategy.Draft{ID: id, Name: "Probe", Raw: new(native),
		ManagedLua: []strategy.LuaReference{{ID: scriptID, Revision: script.Revision(), ApproveImported: true}}})
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value any) json.RawMessage {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	definitions := []json.RawMessage{encode(record.Definition)}
	requests := []zapretactivation.Request{{OutboundRef: "direct", StrategyRef: id, OutboundRevision: strings.Repeat("a", 64)}}
	sourceKey := zapretactivation.LuaSourceKey(scriptID, script.Revision())
	sources := map[string]json.RawMessage{sourceKey: encode(script)}
	selection, err := resolveZapretBatch(definitions, requests, sources)
	if err != nil || !selection.UsesManagedLua {
		t.Fatalf("broker managed Lua selection: %v", err)
	}
	command, err := selection.Compile("/engine", 401, 0x40000000, []uint32{0x120000}, false, false)
	if err != nil || command.Trace != nil || !strings.Contains(command.Lua, script.Source) {
		t.Fatalf("broker managed Lua compilation: %v", err)
	}
	args := strings.Join(command.Args, " ")
	if !strings.Contains(args, "--lua-init=@"+broker.ZapretLuaBundlePath) || !strings.Contains(args, "--filter-mark=0x120000") {
		t.Fatal("managed callback was not compiled inside the owned packet scope")
	}
	sources["other/"+script.Revision()] = encode(script)
	if _, err := resolveZapretBatch(definitions, requests, sources); err == nil {
		t.Fatal("unselected source accepted")
	}
	delete(sources, "other/"+script.Revision())
	script.Source += "\n-- replaced"
	sources[sourceKey] = encode(script)
	if _, err := resolveZapretBatch(definitions, requests, sources); err == nil {
		t.Fatal("substituted source accepted for a different revision")
	}
}
