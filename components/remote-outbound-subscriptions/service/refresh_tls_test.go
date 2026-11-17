//go:build !minimal

package service

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

func TestFailedTLSRefreshKeepsAcceptedSnapshotBindingsAndRedactsSourceCredentials(t *testing.T) {
	initRemoteOutboundTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()
	svc := newTestRemoteOutboundService()
	accepted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"outbounds\":[{\"type\":\"socks\",\"tag\":\"accepted\",\"server\":\"192.0.2.1\",\"server_port\":1080}]}"))
	}))
	defer accepted.Close()
	saved, err := svc.SaveSubscription(remote.RemoteOutboundSubscription{Name: "accepted TLS source", Url: accepted.URL, UserAgent: "tls-fixture/1.0", Enabled: true}, true, "test")
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
	var requests atomic.Int32
	invalid := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	invalid.Config.ErrorLog = log.New(io.Discard, "", 0)
	invalid.StartTLS()
	defer invalid.Close()
	replacement := remote.RemoteOutboundSubscription{Id: saved.Id, Name: "untrusted TLS source", Url: invalid.URL + "/private-source-token?key=fixture-only-value", SourceType: remote.SourceRemote, UserAgent: "tls-fixture/1.0", Enabled: true}
	if _, err := svc.SaveSubscription(replacement, true, "test"); err != nil {
		t.Fatal(err)
	}
	var before remote.RemoteOutboundSubscription
	if err := db.First(&before, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	acceptedState := subscriptionRevisionState(t, db)
	result, err := svc.RefreshSubscriptionContext(context.Background(), saved.Id, "test")
	if result != nil || err == nil || !strings.Contains(err.Error(), "certificate signed by unknown authority") {
		t.Fatalf("untrusted certificate result=%+v err=%v", result, err)
	}
	if requests.Load() != 0 {
		t.Fatal("source TLS validation admitted HTTP request")
	}
	var after remote.RemoteOutboundSubscription
	if err := db.First(&after, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	if after.LastError == "" {
		t.Fatal("TLS failure omitted durable error evidence")
	}
	logs := logger.FilteredLogs(100, "WARN", "panel", "sub:")
	if len(logs) == 0 {
		t.Fatal("TLS failure omitted transport log evidence")
	}
	for _, secret := range []string{"private-source-token", "fixture-only-value"} {
		if strings.Contains(err.Error(), secret) || strings.Contains(after.LastError, secret) || strings.Contains(strings.Join(logs, " "), secret) {
			t.Fatal("TLS failure leaked credential-bearing source URL")
		}
	}
	// The accepted collection and relationships remain byte-for-byte identical.
	// Only documented failure evidence may change on the source row.
	after.LastError, before.LastError = "", ""
	after.LastUpdated, before.LastUpdated = 0, 0
	after.UpdatedAt, before.UpdatedAt = 0, 0
	if !reflect.DeepEqual(before, after) {
		t.Fatal("TLS failure changed accepted source snapshot or source definition")
	}
	actualState := subscriptionRevisionState(t, db)
	delete(acceptedState, "remote_outbound_subscriptions")
	delete(actualState, "remote_outbound_subscriptions")
	if !reflect.DeepEqual(acceptedState, actualState) {
		t.Fatal("TLS failure changed accepted connection/group/membership/Outbound state")
	}
}
