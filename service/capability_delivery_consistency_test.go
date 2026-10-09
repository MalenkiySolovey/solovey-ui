package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
)

func TestCapabilityDeliveryAgreesWithServingEntityForEveryOwnerFact(t *testing.T) {
	uriTypes := map[string]bool{}
	for _, kind := range suburi.EligibleInboundTypes() {
		uriTypes[kind] = true
	}
	for _, fact := range entitycapabilities.Current().Facts {
		if fact.Category != "inbounds" {
			continue
		}
		t.Run(fact.Type, func(t *testing.T) {
			schema, known := clientfacts.Resolve(fact.Type)
			if runtimeprojection.Included(fact.Category, fact.Type) != fact.Available || (saveeligibility.Check(fact.Category, fact.Type, "new", "") == nil) != fact.Available {
				t.Fatal("runtime/save owner disagreement")
			}
			if uriTypes[fact.Type] != (fact.Available && known && schema.URI) {
				t.Fatal("URI delivery disagrees with serving inbound")
			}
			inbound := &model.Inbound{Type: fact.Type, Tag: "local", Options: json.RawMessage("{}"), Addrs: json.RawMessage("[]"), OutJson: json.RawMessage(fmt.Sprintf(`{"type":%q,"tag":"local","server":"example.invalid","server_port":443}`, fact.Type))}
			clientConfig := json.RawMessage("{}")
			if fact.Type == "snell" {
				inbound.OutJson = json.RawMessage(`{"type":"snell","tag":"local","version":6,"psk":"fixture-psk-12","server":"example.invalid","server_port":443}`)
				clientConfig = json.RawMessage(`{"snell":{"userkey":"fixture-client-key"}}`)
			}
			set, err := sublocal.BuildInboundOutbounds(clientConfig, []*model.Inbound{inbound})
			if err != nil {
				t.Fatal(err)
			}
			if (len(set.Outbounds) > 0) != (fact.Available && known && schema.JSON) {
				t.Fatal("JSON delivery disagrees with serving inbound")
			}
		})
	}
}
