package managementcontract

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
)

func TestCurrentActionCatalogParity(t *testing.T) {
	for _, id := range []string{actions.BackupRestore, actions.ComponentRemove} {
		actual, known := Action(id)
		frozen, versioned := wire.LookupAction(id)
		if !known || !versioned || !reflect.DeepEqual(actual, frozen) {
			t.Fatalf("current owner descriptor drift %s: %#v / %#v", id, actual, frozen)
		}
	}
	if _, known := Action("owner.execute"); known {
		t.Fatal("unknown action acquired authority")
	}
}

func TestActualLocalActionInputAndAuthority(t *testing.T) {
	data, err := os.ReadFile("../../contracts/management/v1/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []struct {
			ID       string          `json:"id"`
			Document json.RawMessage `json:"document"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	var command wire.Command
	var remove wire.Command
	for _, vector := range set.Cases {
		if vector.ID == "command-typed-restore" {
			command, err = wire.DecodeCommand(vector.Document)
		}
		if vector.ID == "typed-component-remove" {
			remove, err = wire.DecodeCommand(vector.Document)
		}
	}
	if err != nil || command.CommandID == "" || LocalHost() != (wire.HostRef{Kind: "LOCAL", ID: "local"}) {
		t.Fatalf("Local binding/command: %v", err)
	}
	policy := func(_ context.Context, invocation actions.Invocation) (actions.Decision, error) {
		descriptor, _ := actions.Lookup(invocation.ID)
		return actions.Decision{Allowed: true, Actor: "panel-account:fixture", Permission: descriptor.Permission, Invocation: invocation}, nil
	}
	if _, err := Require(context.Background(), command, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := Require(context.Background(), remove, policy); err != nil {
		t.Fatal(err)
	}
	unsupportedFence := wire.Revision("unproven")
	remove.ExpectedRevision = &unsupportedFence
	if _, err := Require(context.Background(), remove, policy); err == nil {
		t.Fatal("removal silently ignored an unenforceable fence")
	}
	if _, err := Require(context.Background(), command, nil); err == nil {
		t.Fatal("missing actual owner policy was accepted")
	}
	foreign := command
	foreign.Host = wire.HostRef{Kind: "NODE", ID: "node:fixture-01"}
	foreign.Resource.Host = foreign.Host
	if _, err := Require(context.Background(), foreign, policy); err == nil {
		t.Fatal("schema NODE shape enabled production remote execution")
	}
	command.InputDigest = wire.Digest(strings.Repeat("b", 64))
	if _, err := Require(context.Background(), command, policy); err == nil {
		t.Fatal("changed input reused accepted owner authority")
	}
}
