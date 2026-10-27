package runtimeprojection

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestUnavailableInboundGroupDoesNotExposeCanonicalMembers(t *testing.T) {
	rows := []model.Inbound{
		{Type: "core-failover", Tag: "group", Options: json.RawMessage(`{"inbounds":["member"]}`)},
		{Type: "socks", Tag: "member", Options: json.RawMessage(`{}`)},
		{Type: "socks", Tag: "independent", Options: json.RawMessage(`{}`)},
	}
	facts, err := InboundEligibility(rows)
	if err != nil {
		t.Fatal(err)
	}
	if facts["group"].Available || facts["member"].Available || facts["member"].Reason != "INBOUND_GROUP_UNAVAILABLE" {
		t.Fatal("unavailable group exposed a member")
	}
	if !facts["independent"].Available {
		t.Fatal("group projection changed an independent inbound")
	}
}
