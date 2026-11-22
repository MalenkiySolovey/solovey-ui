package outbounds

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func TestZapretDesiredIntentSurvivesUnavailableProjection(t *testing.T) {
	row := model.Outbound{Tag: "direct-a", Type: "direct", Options: json.RawMessage(
		`{"routing_mark":"0x0123","zapret":{"outboundRef":"direct-a","enabled":true,"strategyRef":"fixture-a"}}`)}
	before := string(row.Options)
	if _, err := projectZapretOutbound(row); !errors.Is(err, ErrZapretPacketPathUnqualified) {
		t.Fatalf("enabled intent did not block Core projection: %v", err)
	}
	_, binding, err := zapretOptions(row)
	if err != nil || binding == nil || !binding.Enabled || binding.StrategyRef != "fixture-a" || string(row.Options) != before {
		t.Fatalf("unresolved desired intent was lost: %+v %v", binding, err)
	}
	row.Options = json.RawMessage(strings.Replace(before, `"enabled":true`, `"enabled":false`, 1))
	native, err := projectZapretOutbound(row)
	if err != nil || string(native.Options) != `{"routing_mark":"0x0123"}` || !strings.Contains(string(row.Options), `"zapret"`) {
		t.Fatalf("OFF did not preserve ordinary Direct: %s %v", native.Options, err)
	}
}

func TestZapretAttachmentRejectsAmbiguousOptions(t *testing.T) {
	for _, raw := range []string{
		`{"protect_path":"/run/solovey-ui/zapret/z-stale"}`,
		`{"zapret": null}`,
		`{"zapret":{"outboundRef":"a"}}`,
		`{"zapret":{"outboundRef":"a","enabled":null}}`,
		`{"zapret":{"outboundRef":"a","enabled":true}}`,
		`{"zapret":{"outboundRef":"a","enabled":true,"strategyRef":"a","queue":1}}`,
		`{"zapret":{"outboundRef":"b","enabled":false}}`,
		`{"detour":"proxy","zapret":{"outboundRef":"a","enabled":true,"strategyRef":"a"}}`,
		`{"zapret":{"outboundRef":"a","enabled":true,"enabled":false}}`,
	} {
		if _, err := projectZapretOutbound(model.Outbound{Tag: "a", Type: "direct", Options: json.RawMessage(raw)}); err == nil {
			t.Errorf("invalid attachment was accepted: %s", raw)
		}
	}
}

func TestZapretProjectionRequiresMatchingGenerationGrant(t *testing.T) {
	row := model.Outbound{Tag: "direct-a", Type: "direct", Options: json.RawMessage(
		`{"zapret":{"outboundRef":"direct-a","enabled":true,"strategyRef":"proof"}}`)}
	before := string(row.Options)
	grant := func(request zapretactivation.Request) zapretactivation.Grant {
		t.Helper()
		if request.OutboundRef != row.Tag || request.StrategyRef != "proof" || !zapretactivation.ValidDigest(request.OutboundRevision) {
			t.Fatalf("unexpected grant request: %+v", request)
		}
		return zapretactivation.Grant{Request: request, StrategyRevision: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64)}
	}
	_, err := projectZapretRuntime(row, false, func(request zapretactivation.Request) (zapretactivation.Grant, error) {
		stale := grant(request)
		stale.OutboundRevision = strings.Repeat("c", 64)
		return stale, nil
	})
	if !errors.Is(err, ErrZapretPacketPathUnqualified) {
		t.Fatalf("an unmatched grant entered the runtime config: %v", err)
	}
	projected, err := projectZapretRuntime(row, false, func(request zapretactivation.Request) (zapretactivation.Grant, error) {
		return grant(request), nil
	})
	want := `{"protect_path":"` + zapretactivation.SocketRoot + "/z-" + strings.Repeat("b", 64) + `"}`
	if err != nil || string(projected.Options) != want || string(row.Options) != before {
		t.Fatalf("expected a runtime-only socket path, got %s, %v", projected.Options, err)
	}
}

func TestUnavailableZapretStopsBatchBeforeAnyLiveRemoval(t *testing.T) {
	db := newFailoverDB(t)
	rows := []model.Outbound{
		{Tag: "direct-b", Type: "direct", Options: json.RawMessage(`{}`)},
		{Tag: "direct-a", Type: "direct", Options: json.RawMessage(`{"zapret":{"outboundRef":"direct-a","enabled":true,"strategyRef":"a"}}`)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := ValidateStored(db); err != nil {
		t.Fatalf("unavailable dependency is not a malformed stored attachment: %v", err)
	}
	if projected, err := GetAllConfig(db); !errors.Is(err, ErrZapretPacketPathUnqualified) || projected != nil {
		t.Fatalf("full projection discarded an unresolved attachment: %s, %v", projected, err)
	}
	core := &fakeOutboundCore{running: true}
	if err := Restart(db, []uint{rows[0].Id, rows[1].Id}, core); !errors.Is(err, ErrZapretPacketPathUnqualified) || len(core.calls) != 0 {
		t.Fatalf("unqualified binding crossed the runtime boundary: %v, calls=%q", err, core.calls)
	}
	if err := requireExplicitZapretChange(rows[1], model.Outbound{Tag: "direct-a", Type: "direct"}); !errors.Is(err, errZapretChangeRequired) {
		t.Fatalf("ordinary edit could silently drop the saved binding: %v", err)
	}
	var saved model.Outbound
	if err := db.First(&saved, rows[1].Id).Error; err != nil || !bytes.Equal(saved.Options, rows[1].Options) {
		t.Fatalf("runtime rejection modified stored desired options: %v", err)
	}
}
