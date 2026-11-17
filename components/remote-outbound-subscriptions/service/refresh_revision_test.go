//go:build !minimal

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	"gorm.io/gorm"
)

func TestInFlightRefreshCannotOverwriteReplacementSourceOrItsAcceptedBindings(t *testing.T) {
	testInFlightSourceMutation(t, false)
}

func TestInFlightRefreshCannotRecreateDeletedSourceOrItsRemovedBindings(t *testing.T) {
	testInFlightSourceMutation(t, true)
}

func testInFlightSourceMutation(t *testing.T, deleteSource bool) {
	t.Helper()
	for _, failFetch := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful old fetch", true: "failed old fetch"}[failFetch], func(t *testing.T) {
			initRemoteOutboundTestDB(t)
			t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
			db := dbsqlite.DB()
			service := newTestRemoteOutboundService()
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var requests atomic.Int32
			const accepted = `{"outbounds":[{"type":"socks","tag":"accepted","server":"192.0.2.1","server_port":1080}]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				n := requests.Add(1)
				if n == 2 {
					close(entered)
					select {
					case <-release:
					case <-request.Context().Done():
						return
					}
					if failFetch {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					_, _ = w.Write([]byte(strings.ReplaceAll(accepted, "192.0.2.1", "192.0.2.2")))
					return
				}
				if n != 1 {
					t.Error("stale refresh retried the replacement source")
				}
				_, _ = w.Write([]byte(accepted))
			}))
			defer server.Close()
			defer unblock()
			saved, err := service.SaveSubscription(remote.RemoteOutboundSubscription{Name: "original source", Url: server.URL, UserAgent: "revision-fixture/1.0", Enabled: true}, true, "test")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.RefreshSubscriptionContext(context.Background(), saved.Id, "test"); err != nil {
				t.Fatal(err)
			}
			var connection remote.RemoteOutboundConnection
			if err := db.Where("subscription_id = ?", saved.Id).First(&connection).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.SyncConnectionToOutbound(connection.Id, "test"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			type refreshResult struct {
				value *remote.RefreshResult
				err   error
			}
			done := make(chan refreshResult, 1)
			go func() {
				value, err := service.RefreshSubscriptionContext(ctx, saved.Id, "test")
				done <- refreshResult{value, err}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("old source fetch did not enter", ctx.Err())
			}
			if deleteSource {
				if err := service.DeleteSubscription(saved.Id, "test"); err != nil {
					t.Fatal(err)
				}
			} else {
				replacement := remote.RemoteOutboundSubscription{Id: saved.Id, Name: "replacement source", Url: server.URL + "/replacement", SourceType: remote.SourceRemote, UserAgent: "revision-fixture/1.0", Enabled: true}
				if _, err := service.SaveSubscription(replacement, true, "test"); err != nil {
					t.Fatal(err)
				}
			}
			before := subscriptionRevisionState(t, db)
			unblock()
			select {
			case result := <-done:
				rejected := errors.Is(result.err, gorm.ErrRecordNotFound)
				if !deleteSource {
					rejected = result.err != nil && strings.Contains(result.err.Error(), "SOURCE_CHANGED_RETRY")
				}
				if result.value != nil || !rejected {
					t.Fatalf("stale fetch result=%+v err=%v", result.value, result.err)
				}
			case <-ctx.Done():
				t.Fatal("stale fetch did not finish", ctx.Err())
			}
			if requests.Load() != 2 {
				t.Fatal("stale fetch was silently replayed")
			}
			if after := subscriptionRevisionState(t, db); !reflect.DeepEqual(before, after) {
				t.Fatal("old fetch changed replacement source, accepted snapshot, relationships or Outbound")
			}
		})
	}
}

func subscriptionRevisionState(t *testing.T, db *gorm.DB) map[string]string {
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
