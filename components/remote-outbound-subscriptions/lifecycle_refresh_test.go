//go:build !minimal

package remoteoutboundsubscriptions

import (
	"context"
	"net"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestActualComponentAdmissionAndDisableCancelScheduledFetchWithoutChangingAcceptedState(t *testing.T) {
	// The real production scheduler retains its one-minute cadence. Virtual time
	// drives that cadence without a wall-clock delay or a second scheduler.
	synctest.Test(t, func(t *testing.T) {
		initRemoteComponentSettingTestDB(t)
		locallyResetRemoteHooks(t)
		if err := (component{}).Migrate(t.Context(), lifecycle.Context{}); err != nil {
			t.Fatal(err)
		}
		seedSubscriptionCompatibilityDB(t)
		db := dbsqlite.DB()
		// The selected transport blocks before a connection exists. No external
		// destination is dialed, and cancellation crosses the actual HTTP adapter.
		if err := db.Model(&remote.RemoteOutboundSubscription{}).Where("id = ?", 41).Updates(map[string]any{
			"url": "http://93.184.216.34/subscription", "user_agent": "lifecycle-fixture/1.0", "download_outbound_id": 81,
			"source_headers": "", "last_updated": 0, "update_interval": 60,
		}).Error; err != nil {
			t.Fatal(err)
		}
		installed := t.TempDir() + "/installed.json"
		t.Setenv(installstate.InstalledFileEnv, installed)
		metadata := installstate.Metadata{Version: 1, Profile: "full", Binary: "full"}
		if err := installstate.Store(installed, metadata); err != nil {
			t.Fatal(err)
		}
		enabledKey := enabledstate.SettingKey(id)
		if err := db.Create(&model.Setting{Key: enabledKey, Value: "false"}).Error; err != nil {
			t.Fatal(err)
		}
		before := subscriptionCompatibilityDigests(t)
		var clients, dials atomic.Int32
		entered := make(chan struct{})
		canceled := make(chan struct{})
		host := supervisor.New(componenthost.Deps{API: componenthost.APIDeps{
			Runtime: &service.Runtime{},
			OutboundHTTP: componenthost.OutboundHTTPClientFunc(func(tag string, _ time.Duration) (*http.Client, error) {
				if tag != "accepted-node" {
					t.Errorf("selected download tag = %q", tag)
				}
				clients.Add(1)
				return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					if dials.Add(1) != 1 {
						t.Error("source started duplicate scheduled fetch")
					} else {
						close(entered)
					}
					<-ctx.Done()
					close(canceled)
					return nil, ctx.Err()
				}}}, nil
			}),
		}})
		defer func() {
			if err := host.Stop(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		hooksRegistered := func() bool {
			runtimeHooks.Lock()
			defer runtimeHooks.Unlock()
			return runtimeHooks.unregisterClientOutbounds != nil || runtimeHooks.unregisterOutboundSave != nil || runtimeHooks.unregisterOutboundDelete != nil || runtimeHooks.unregisterMetadata != nil || runtimeHooks.unregisterBackup != nil || runtimeHooks.unregisterOptionKeys != nil || runtimeHooks.unregisterSettings != nil
		}
		for _, installedState := range []bool{false, true} {
			if installedState {
				metadata.Components = []installstate.InstalledComponent{{ID: id, Delivery: componentManifest.Delivery, Installed: true}}
			}
			if err := installstate.Store(installed, metadata); err != nil {
				t.Fatal(err)
			}
			if err := host.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2 * time.Minute)
			synctest.Wait()
			if hooksRegistered() || clients.Load() != 0 || dials.Load() != 0 {
				t.Fatalf("inactive component executed: installed=%v clients=%d dials=%d", installedState, clients.Load(), dials.Load())
			}
			if after := subscriptionCompatibilityDigests(t); !reflect.DeepEqual(before, after) {
				t.Fatal("inactive component changed durable state")
			}
		}
		if err := db.Model(&model.Setting{}).Where("key = ?", enabledKey).Update("value", "true").Error; err != nil {
			t.Fatal(err)
		}
		if err := host.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !hooksRegistered() {
			t.Fatal("enabled component did not install its actual runtime hooks")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		select {
		case <-entered:
		default:
			t.Fatal("actual scheduler did not enter admitted source fetch")
		}
		if clients.Load() != 1 || dials.Load() != 1 {
			t.Fatal("enabled scheduler did not use one selected transport")
		}
		if err := db.Model(&model.Setting{}).Where("key = ?", enabledKey).Update("value", "false").Error; err != nil {
			t.Fatal(err)
		}
		if err := host.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-canceled:
		default:
			t.Fatal("component disable did not cancel and drain actual source I/O")
		}
		if hooksRegistered() {
			t.Fatal("disabled component retained active hooks")
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if clients.Load() != 1 || dials.Load() != 1 {
			t.Fatal("disabled scheduler restarted source I/O")
		}
		if after := subscriptionCompatibilityDigests(t); !reflect.DeepEqual(before, after) {
			t.Fatal("disable during fetch changed accepted snapshots, relationships, settings or Outbound")
		}
	})
}
