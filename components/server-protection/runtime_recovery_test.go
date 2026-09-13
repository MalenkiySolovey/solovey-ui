//go:build !minimal

package serverprotection

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	firewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

// This component lifetime fixture does not manufacture a SQLite lock. Real
// SQLite contention and full four-resource restoration are tested by firewall.
type startupAuthorityFailure struct {
	*repository.Repository
	reads  int
	failAt int
}

func (s *startupAuthorityFailure) FirewallAuthority(ctx context.Context) (repository.FirewallAuthoritySnapshot, error) {
	s.reads++
	if s.reads == s.failAt {
		return repository.FirewallAuthoritySnapshot{}, errors.New("controlled authority read failure")
	}
	return s.Repository.FirewallAuthority(ctx)
}

type startupAbsentHelper struct{}

func (startupAbsentHelper) Execute(context.Context, helper.Request) (helper.Response, error) {
	return helper.Response{OK: true, NFT: &helper.NFTResult{}}, nil
}

// These dependencies are required for construction but must never be called
// in a pre-mutation failure. A call would panic and fail the fixture.
type startupUnusedDependencies struct {
	firewall.ArtifactService
	firewall.MutationMarker
	firewall.StateStore
	firewall.RecoveryBundler
}

func TestComponentRuntimeExecutionFailureKeepsRecoveryAndSchedules(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("authority-read-%d", failAt), func(t *testing.T) {
			assertComponentRuntimeExecutionFailureKeepsRecoveryAndSchedules(t, failAt)
		})
	}
}

func assertComponentRuntimeExecutionFailureKeepsRecoveryAndSchedules(t *testing.T, failAt int) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	markServerProtectionInstalled(t, dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "startup.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	c := component{}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	if err := c.Migrate(t.Context(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(dbsqlite.DB())
	manager := operations.NewManager(repo, operations.Options{InstanceID: "component-recovery", PID: 9876, RecoveryEvery: time.Hour})
	row := repository.OperationLockModel{OperationID: "component-runtime-recovery", Kind: operations.KindFirewall, State: operations.StateApplied, Revision: 1, IdempotencyKey: "component-runtime-recovery", ResourceID: "managed-table:inet:solovey_protection"}
	if err := dbsqlite.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	store := &startupAuthorityFailure{Repository: repo, failAt: failAt}
	unused := startupUnusedDependencies{}
	health := func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result { return nil }
	workflow := &firewall.Workflow{Manager: manager, Helper: startupAbsentHelper{}, Contributions: store, Artifacts: unused, Marker: unused, State: unused, Recovery: unused, Health: health, RollbackHealth: health}
	hooks.Lock()
	hooks.operationManager, hooks.firewallWorkflow = manager, workflow
	hooks.Unlock()
	scheduler := &trackingScheduler{}
	if err := c.Start(t.Context(), serverProtectionLifecycleContext(scheduler)); err != nil {
		t.Fatalf("runtime execution destroyed component: %v", err)
	}
	stored, err := repo.OperationByID(t.Context(), row.OperationID)
	if err != nil || stored.RecoveryAttempts != 1 || stored.RecoveryErrorCode != "runtime_restore_execution_failed" {
		t.Fatalf("failure not retained: %+v %v", stored, err)
	}
	if !slices.Equal(scheduler.added, []string{"@every 1m", "@every 1h"}) {
		t.Fatalf("schedules unavailable: %v", scheduler.added)
	}
	hooks.Lock()
	ownerPresent := hooks.operationManager == manager && hooks.hostSurfaceCancel != nil && hooks.artifactPruner != nil
	hooks.Unlock()
	if !ownerPresent {
		t.Fatal("runtime owners were cleaned up")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(scheduler.removed) != 2 {
		t.Fatal("component stop failed to drain schedules")
	}
}
