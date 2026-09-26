//go:build linux && !minimal

package serverprotection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestLinuxProductionHelperCompositionWithoutBrokerPersistsHelperCallFailed(t *testing.T) {
	if _, err := os.Lstat(broker.DefaultSocketPath); err == nil {
		t.Skip("requires the fixed production broker socket to be absent")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect fixed production broker socket: %v", err)
	}
	if invoker, reason := protectionhelper.DiscoverInstalledBrokerInvoker(); invoker == nil || reason != "" {
		t.Fatalf("Linux production helper invoker was not composed: invoker=%T reason=%q", invoker, reason)
	}

	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	markServerProtectionInstalled(t, dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })

	c := component{}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	if err := c.Migrate(context.Background(), serverProtectionLifecycleContext(t, nil)); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := c.Start(context.Background(), serverProtectionLifecycleContext(t, nil)); err != nil {
		t.Fatalf("Start: %v", err)
	}

	hooks.Lock()
	workflowComposed := hooks.helperClient != nil && hooks.firewallWorkflow != nil && hooks.firewallWorkflow.Helper != nil
	hooks.Unlock()
	if !workflowComposed {
		t.Fatal("Linux production firewall workflow was not composed")
	}
	authority, err := protectionrepository.New(dbsqlite.DB()).FirewallAuthority(t.Context())
	if err != nil || !authority.HasObservation || authority.Observation.State != protectionfirewall.FirewallLiveUnavailable || authority.Observation.Reason != protectionfirewall.FirewallReasonHelperCallFailed {
		t.Fatalf("absent broker did not persist post-composition helper failure: authority=%#v err=%v", authority, err)
	}
}
