//go:build !minimal

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
)

func TestUnusableProviderResponsesKeepAcceptedSnapshotAndWorkingBindings(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "empty response", status: http.StatusOK},
		{name: "zero endpoints", body: "{\"outbounds\":[]}", status: http.StatusOK},
		{name: "malformed JSON", body: "{\"outbounds\":[", status: http.StatusOK},
		{name: "malformed YAML", body: "proxies: [", status: http.StatusOK},
		{name: "failed HTTP", body: "provider unavailable", status: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initRemoteOutboundTestDB(t)
			t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
			db := dbsqlite.DB()
			svc := newTestRemoteOutboundService()
			var fail atomic.Bool
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if fail.Load() {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				_, _ = w.Write([]byte("{\"outbounds\":[{\"type\":\"socks\",\"tag\":\"accepted\",\"server\":\"192.0.2.1\",\"server_port\":1080}]}"))
			}))
			defer server.Close()
			saved, err := svc.SaveSubscription(remote.RemoteOutboundSubscription{Name: "provider response fixture", Url: server.URL, UserAgent: "provider-fixture/1.0", Enabled: true}, true, "test")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err != nil {
				t.Fatal(err)
			}
			var connection remote.RemoteOutboundConnection
			if err := db.Where("subscription_id = ?", saved.Id).First(&connection).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SyncConnectionToOutbound(connection.Id, "test"); err != nil {
				t.Fatal(err)
			}
			var before remote.RemoteOutboundSubscription
			if err := db.First(&before, saved.Id).Error; err != nil {
				t.Fatal(err)
			}
			accepted := subscriptionRevisionState(t, db)
			fail.Store(true)
			result, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test")
			if result != nil || err == nil {
				t.Fatalf("unusable provider response accepted: result=%+v err=%v", result, err)
			}
			if requests.Load() != 2 {
				t.Fatal("failed explicit representation was replayed")
			}
			var after remote.RemoteOutboundSubscription
			if err := db.First(&after, saved.Id).Error; err != nil {
				t.Fatal(err)
			}
			if after.LastError == "" {
				t.Fatal("provider failure omitted durable evidence")
			}
			before.LastError, after.LastError = "", ""
			before.LastUpdated, after.LastUpdated = 0, 0
			before.UpdatedAt, after.UpdatedAt = 0, 0
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unusable response changed accepted source or snapshot")
			}
			actual := subscriptionRevisionState(t, db)
			delete(accepted, "remote_outbound_subscriptions")
			delete(actual, "remote_outbound_subscriptions")
			if !reflect.DeepEqual(accepted, actual) {
				t.Fatal("unusable response changed groups, memberships, connections or working Outbound")
			}
		})
	}
}
