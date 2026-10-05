package clientfacts

import (
	"regexp"
	"testing"

	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
)

func TestClientFieldsAreImmutableAndSafeBeforeSQL(t *testing.T) {
	fieldPattern := regexp.MustCompile(`^[a-z0-9_]+$`)
	for _, fact := range Facts() {
		if field, ok := UserField(fact.Type); !ok || field != fact.UserField || !fieldPattern.MatchString(field) {
			t.Fatalf("unsafe field for %q", fact.Type)
		}
	}
	fields := Facts()
	fields[0].UserField = "vmess') FROM clients; --"
	if field, _ := UserField(fields[0].Type); field == fields[0].UserField {
		t.Fatal("caller changed authoritative credential schema")
	}
	if _, ok := UserField("vmess'); DROP TABLE clients; --"); ok {
		t.Fatal("untrusted type became a SQL field")
	}
	if field, ok := UserField("shadowsocks16"); !ok || field != "shadowsocks" {
		t.Fatal("credential variant became a runtime discriminator")
	}
	if CanDeliver("shadowsocks16", "json") {
		t.Fatal("credential variant falsely authorized as a runtime inbound")
	}
}

func TestDeliveryEligibilityUsesServingInboundAndPreservesSchema(t *testing.T) {
	eligible := map[string]bool{}
	for _, name := range URITypes(true) {
		eligible[name] = true
	}
	for _, fact := range Facts() {
		available := entitycapabilities.Resolve("inbounds", fact.Type).Available
		if CanDeliver(fact.Type, "json") != available || CanDeliver(fact.Type, "uri") != (available && fact.URI) || eligible[fact.Type] != (available && fact.URI) {
			t.Fatalf("delivery disagrees for %q", fact.Type)
		}
	}
	if !CanDeliver("naive", "uri") {
		t.Fatal("server Naive inbound must not depend on its outbound build flag")
	}
	if _, ok := Resolve("unknown-history"); ok || CanDeliver("unknown-history", "json") {
		t.Fatal("unknown historical schema became eligible")
	}
	if CanDeliver("vmess", "unknown-format") {
		t.Fatal("unknown delivery format became eligible")
	}
}
