//go:build linux && !minimal

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func TestSelectedZapretListsKeepRevisionAcrossBoundedBrokerFrame(t *testing.T) {
	const id = "ui-zapret-selected-lists"
	value, err := strategy.ParseNative(id, "--filter-tcp=18080 --lua-desync=pass")
	if err != nil {
		t.Fatal(err)
	}
	domains := make([]string, 40000)
	for i := range domains {
		domains[i] = fmt.Sprintf("fixture-%05d.large-list.example.test", i)
	}
	value.Profiles[0].Lists = &strategy.NativeLists{HostList: domains, HostListExclude: []string{"exclude.example.test"}}
	value, err = strategy.Seal(value)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := json.Marshal(value)
	if err != nil || len(definition) <= broker.MaxRequestBytes {
		t.Fatalf("fixture must exceed the unchanged broker frame: %d, %v", len(definition), err)
	}
	input := zapretactivation.Validation{
		Bindings:    []zapretactivation.Request{{OutboundRef: "direct", StrategyRef: id, OutboundRevision: strings.Repeat("a", 64)}},
		Definitions: map[string]json.RawMessage{id: definition},
	}
	packed, err := zapretactivation.PackSelectedLists(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input.Definitions[id], definition) || bytes.Contains(packed.Definitions[id], []byte(domains[0])) {
		t.Fatal("packing mutated the stored definition or left inline members")
	}
	var frame bytes.Buffer
	if err := broker.WriteFrame(&frame, packed, broker.MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	var received zapretactivation.Validation
	if err := broker.ReadFrame(&frame, &received, broker.MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	var charged int
	reserve := func(_ int, lines int) error { charged += lines; return nil }
	inputs := []json.RawMessage{received.Definitions[id]}
	selection, err := resolveZapretBatch(inputs, received.Bindings, nil, reserve)
	if err != nil || charged != 40001 || selection.Intents[0].StrategyRevision != value.Revision {
		t.Fatalf("selected list content changed its sealed revision or charge: %d, %v", charged, err)
	}
	command, err := selection.Compile("/engine", "/tmp/zapret-test-lists", 401, 0x40000000, []uint32{0x120000}, false, false)
	if err != nil || len(command.ListFiles) != 2 || command.ListFiles[0] != strings.Join(domains, "\n")+"\n" ||
		!slices.Contains(command.Args, "--hostlist=/tmp/zapret-test-lists/list-0") {
		t.Fatalf("broker did not compile only the verified native lists: %v", err)
	}
	denied := errors.New("controlled workspace budget exhausted")
	if _, err := resolveZapretBatch(inputs, received.Bindings, nil, func(int, int) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("broker unpacked before workspace admission: %v", err)
	}
}
