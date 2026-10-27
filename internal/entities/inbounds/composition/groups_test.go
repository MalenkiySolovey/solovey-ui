package composition

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestGroupsKeepOneCanonicalOwnerWithoutCopyingChildren(t *testing.T) {
	rows := []model.Inbound{
		{Id: 1, Type: "socks", Tag: "first", Options: json.RawMessage(`{"listen_port":1080}`)},
		{Id: 2, Type: "trojan", Tag: "second"},
		{Id: 3, Type: "bond", Tag: "bundle", Options: json.RawMessage(`{"inbounds":["second","first"]}`)},
	}
	before, _ := json.Marshal(rows)
	index, err := Build(rows)
	if err != nil {
		t.Fatal(err)
	}
	if index.Root("first") != "bundle" || index.Root("second") != "bundle" || index.Root("ordinary") != "ordinary" {
		t.Fatal("member reapply did not resolve to its canonical root")
	}
	if got := index.Members("bundle"); !reflect.DeepEqual(got, []string{"second", "first"}) {
		t.Fatal("member order changed")
	}
	returned := index.Members("bundle")
	returned[0] = "changed"
	if index.Members("bundle")[0] != "second" {
		t.Fatal("caller mutated canonical membership")
	}
	after, _ := json.Marshal(rows)
	if string(before) != string(after) {
		t.Fatal("building composition mutated persisted state")
	}
}

func TestGroupsRejectAmbiguousOwnershipAndInvalidReferences(t *testing.T) {
	for name, groups := range map[string][]model.Inbound{
		"missing":         {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":["missing"]}`)}},
		"self":            {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":["root"]}`)}},
		"duplicate":       {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":["member","member"]}`)}},
		"shared":          {{Type: "bond", Tag: "one", Options: json.RawMessage(`{"inbounds":["member"]}`)}, {Type: "core-failover", Tag: "two", Options: json.RawMessage(`{"inbounds":["member"]}`)}},
		"nested":          {{Type: "bond", Tag: "one", Options: json.RawMessage(`{"inbounds":["two"]}`)}, {Type: "core-failover", Tag: "two", Options: json.RawMessage(`{"inbounds":["member"]}`)}},
		"embedded-copy":   {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":[{"type":"socks","tag":"member"}]}`)}},
		"empty":           {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":[]}`)}},
		"null-member":     {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":[null]}`)}},
		"duplicate-field": {{Type: "bond", Tag: "root", Options: json.RawMessage(`{"inbounds":["member"],"inbounds":["member"]}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			rows := append([]model.Inbound{{Id: 1, Type: "socks", Tag: "member"}}, groups...)
			if _, err := Build(rows); err == nil {
				t.Fatal("accepted invalid/ambiguous group")
			}
		})
	}
}

func TestOrdinaryFailoverPolicyIsNotAnInboundComposition(t *testing.T) {
	if IsGroup("failover") {
		t.Fatal("canonical Failover policy was reinterpreted as a core group")
	}
	if _, err := Members("socks", json.RawMessage(`not-group-options`)); err != nil {
		t.Fatal(err)
	}
}
