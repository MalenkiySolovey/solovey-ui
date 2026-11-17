//go:build !minimal

package backendoutbounds_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/backendoutbounds"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/bindingfixture"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"gorm.io/gorm"
)

func TestActualBindingRechecksAuthorityAfterPreviewWithoutChangingAcceptedState(t *testing.T) {
	cases := []struct {
		name     string
		change   func(*hostoutbounds.Candidate, *coreprovider.Identity)
		canceled bool
		want     error
	}{
		{name: "missing Host", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) { c.Binding.Host = hostref.Ref{} }, want: hostoutbounds.ErrScope},
		{name: "foreign source Host", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) {
			c.Binding.Host = hostref.Ref{Kind: "NODE", ID: "offline-node"}
		}, want: hostoutbounds.ErrScope},
		{name: "foreign resource Host", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) {
			c.Resource.Host = hostref.Ref{Kind: "NODE", ID: "offline-node"}
		}, want: hostoutbounds.ErrScope},
		{name: "different source", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) { c.Binding.SourceID = "other-source" }, want: hostoutbounds.ErrScope},
		{name: "different endpoint", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) { c.Binding.EndpointID = "other-endpoint" }, want: hostoutbounds.ErrScope},
		{name: "different resource", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) { c.Resource.ID++ }, want: hostoutbounds.ErrScope},
		{name: "provider lost after preview", change: func(_ *hostoutbounds.Candidate, p *coreprovider.Identity) {
			*p = coreprovider.Identity{State: coreprovider.Unknown}
		}, want: hostoutbounds.ErrProvider},
		{name: "provider authority becomes unknown", change: func(_ *hostoutbounds.Candidate, p *coreprovider.Identity) { p.State = coreprovider.Unknown }, want: hostoutbounds.ErrCapability},
		{name: "unknown protocol capability", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) {
			c.Definition = json.RawMessage(`{"type":"unavailable-fixture-protocol","tag":"managed-node","server":"example.org","server_port":443}`)
		}, want: hostoutbounds.ErrCapability},
		{name: "unsupported nested transport", change: func(c *hostoutbounds.Candidate, _ *coreprovider.Identity) {
			c.Definition = json.RawMessage(`{"type":"vless","tag":"managed-node","server":"example.org","server_port":443,"transport":{"type":"unavailable-fixture-transport"}}`)
		}, want: hostoutbounds.ErrCapability},
		{name: "operation canceled after preview", canceled: true, want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, source, definition := fixture(t)
			row := model.Outbound{Tag: source.OutboundTag, Type: source.Type, SortOrder: 17, Options: json.RawMessage(`{"server":"example.org","server_port":443,"bind_interface":"local-user-choice"}`)}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			source.OutboundId = &row.Id
			if err := db.Table("remote_outbound_connections").Where("id = ?", source.Id).Update("outbound_id", row.Id).Error; err != nil {
				t.Fatal(err)
			}
			// Start with the same trusted provider identity used by the actual adapter.
			owner := backendoutbounds.New(db, source)
			candidate, err := owner.Prepare(db.Statement.Context, hostoutbounds.Upsert, definition, hostoutbounds.Missing{})
			if err != nil {
				t.Fatal(err)
			}
			provider := bindingfixture.Provider(t)
			ctx, cancel := context.WithCancel(backendoutbounds.WithProviderObservation(context.Background(), func() coreprovider.Identity { return provider }))
			defer cancel()
			if _, err := hostoutbounds.PreviewBinding(ctx, candidate, owner); err != nil {
				t.Fatal(err)
			}
			before := actualBindingState(t, db)
			if tc.change != nil {
				tc.change(&candidate, &provider)
			}
			if tc.canceled {
				cancel()
			}
			receipt, err := hostoutbounds.ApplyBinding(ctx, candidate, owner)
			if !errors.Is(err, tc.want) || receipt.ApplyAttempted || receipt.State != hostoutbounds.Failed {
				t.Fatalf("authority rejection = %+v %v, want %v before any apply", receipt, err, tc.want)
			}
			if after := actualBindingState(t, db); !reflect.DeepEqual(before, after) {
				t.Fatal("rejected binding changed source, relationships or local Outbound")
			}
		})
	}
}

func actualBindingState(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{"remote_outbound_subscriptions", "remote_outbound_connections", "remote_outbound_groups", "remote_outbound_group_connections", "outbounds"} {
		var rows []map[string]any
		if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		result[table] = string(data)
	}
	return result
}
