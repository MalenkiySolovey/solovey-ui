//go:build !minimal

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
)

func TestScheduledRefreshDrainsBeforeRestoreAndCanceledFetchKeepsAcceptedState(t *testing.T) {
	initRemoteOutboundTestDB(t)
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	db := dbsqlite.DB()
	svc := newTestRemoteOutboundService()
	download := model.Outbound{Tag: "restore-download", Type: "direct", Options: json.RawMessage("{}")}
	if err := db.Create(&download).Error; err != nil {
		t.Fatal(err)
	}
	var requests, dials atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) != 1 {
			t.Error("canceled source opened another HTTP request")
		}
		_, _ = w.Write([]byte(`{"outbounds":[{"type":"socks","tag":"accepted","server":"192.0.2.1","server_port":1080}]}`))
	}))
	defer server.Close()
	entered := make(chan struct{})
	svc.OutboundHTTP = componenthost.OutboundHTTPClientFunc(func(string, time.Duration) (*http.Client, error) {
		return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if dials.Add(1) == 1 {
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}}}, nil
	})
	saved, err := svc.SaveSubscription(remote.RemoteOutboundSubscription{Name: "restore source", Url: server.URL, UserAgent: "restore-fixture/1.0", DownloadOutboundID: download.Id, Enabled: true, AutoUpdate: true, UpdateInterval: 60}, true, "test")
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
	if err := db.Model(&remote.RemoteOutboundSubscription{}).Where("id = ?", saved.Id).Update("last_updated", 0).Error; err != nil {
		t.Fatal(err)
	}
	var before remote.RemoteOutboundSubscription
	var beforeConnection remote.RemoteOutboundConnection
	var beforeOutbound model.Outbound
	if err := db.First(&before, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&beforeConnection, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&beforeOutbound, *beforeConnection.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	refreshCtx, stopRefresh := context.WithCancel(ctx)
	defer stopRefresh()
	type refreshResult struct {
		count int
		err   error
	}
	refreshed := make(chan refreshResult, 1)
	go func() {
		count, err := svc.RefreshDueSubscriptionsContext(refreshCtx, "test")
		refreshed <- refreshResult{count, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("scheduled fetch did not enter", ctx.Err())
	}
	type maintenanceResult struct {
		owner *dbsqlite.Maintenance
		err   error
	}
	maintained := make(chan maintenanceResult, 1)
	go func() { owner, err := dbsqlite.BeginMaintenance(ctx); maintained <- maintenanceResult{owner, err} }()
	// This is the actual restore owner's admission freeze, with the scheduled
	// source operation suspended in Backend HTTP I/O between database calls.
	for {
		_, release, err := dbsqlite.AcquireOperation(ctx)
		if errors.Is(err, dbsqlite.ErrMaintenance) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		release()
		runtime.Gosched()
	}
	select {
	case result := <-maintained:
		if result.owner != nil {
			result.owner.End()
		}
		t.Fatalf("restore crossed an active source fetch: %v", result.err)
	default:
	}
	stopRefresh()
	select {
	case result := <-refreshed:
		if result.count != 0 || result.err != nil && !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled refresh reported applied work: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("source cancellation did not drain", ctx.Err())
	}
	var admitted maintenanceResult
	select {
	case admitted = <-maintained:
	case <-ctx.Done():
		t.Fatal("restore did not observe released source operation", ctx.Err())
	}
	if admitted.err != nil {
		t.Fatal(admitted.err)
	}
	admitted.owner.End()
	if dials.Load() != 2 || requests.Load() != 1 {
		t.Fatal("canceled refresh spawned another source request")
	}
	var after remote.RemoteOutboundSubscription
	var afterConnection remote.RemoteOutboundConnection
	var afterOutbound model.Outbound
	if err := db.First(&after, saved.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&afterConnection, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&afterOutbound, beforeOutbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeConnection, afterConnection) || !reflect.DeepEqual(beforeOutbound, afterOutbound) {
		t.Fatal("canceled refresh changed accepted data before restore")
	}
}
