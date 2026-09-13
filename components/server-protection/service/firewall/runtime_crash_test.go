package firewall

import (
	"context"
	"testing"

	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const runtimeCrash = "test process interruption"

type crashExpiredRuntime struct {
	RuntimeStore
	after     bool
	fired     bool
	interrupt func()
}

func (s *crashExpiredRuntime) UpdateFirewallRuntime(ctx context.Context, id string, revision int, composition string, before, after repository.FirewallRuntimeBinding) error {
	if !s.fired && after.State == "RESTORE_FAILED" {
		s.fired = true
		if s.after {
			if err := s.RuntimeStore.UpdateFirewallRuntime(ctx, id, revision, composition, before, after); err != nil {
				return err
			}
		}
		if s.interrupt != nil {
			s.interrupt()
		}
		panic(runtimeCrash)
	}
	return s.RuntimeStore.UpdateFirewallRuntime(ctx, id, revision, composition, before, after)
}

type crashRetirementCommit struct {
	FirewallContributionStore
	after     bool
	fired     bool
	interrupt func()
}

func (s *crashRetirementCommit) CommitFirewallAuthority(ctx context.Context, id, compositionRevision, contributionRevision string, replacement *repository.FirewallContributionModel, composition repository.FirewallCompositionModel, state string) error {
	if !s.fired && state == "ROLLED_BACK" {
		s.fired = true
		if s.after {
			if err := s.FirewallContributionStore.CommitFirewallAuthority(ctx, id, compositionRevision, contributionRevision, replacement, composition, state); err != nil {
				return err
			}
		}
		if s.interrupt != nil {
			s.interrupt()
		}
		panic(runtimeCrash)
	}
	return s.FirewallContributionStore.CommitFirewallAuthority(ctx, id, compositionRevision, contributionRevision, replacement, composition, state)
}

func TestRuntimeSQLiteCrashWindows(t *testing.T) {
	for _, window := range []string{"before_fence", "after_fence", "before_checkpoint", "after_checkpoint", "before_health_commit", "after_health_commit"} {
		t.Run(window, func(t *testing.T) { assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_crash_"+window) })
	}
}

type crashRuntimeStore struct {
	RuntimeStore
	window string
	fired  bool
}

func (s *crashRuntimeStore) UpdateFirewallRuntime(ctx context.Context, id string, revision int, composition string, before, after repository.FirewallRuntimeBinding) error {
	boundary := after.State == "RESTORING_RUNTIME" && (s.window == "before_fence" || s.window == "after_fence") || before.State == "RESTORING_RUNTIME" && after.State == "HEALTH_VERIFIED" && (s.window == "before_health_commit" || s.window == "after_health_commit")
	if !s.fired && boundary {
		s.fired = true
		if s.window == "after_fence" || s.window == "after_health_commit" {
			if err := s.RuntimeStore.UpdateFirewallRuntime(ctx, id, revision, composition, before, after); err != nil {
				return err
			}
		}
		panic(runtimeCrash)
	}
	return s.RuntimeStore.UpdateFirewallRuntime(ctx, id, revision, composition, before, after)
}

type crashRuntimeCheckpoint struct {
	StateStore
	after bool
	fired bool
}

func (s *crashRuntimeCheckpoint) WriteFirewallState(id string, data []byte) error {
	if !s.fired {
		s.fired = true
		if s.after {
			if err := s.StateStore.WriteFirewallState(id, data); err != nil {
				return err
			}
		}
		panic(runtimeCrash)
	}
	return s.StateStore.WriteFirewallState(id, data)
}

func runUntilRuntimeCrash(t *testing.T, start func() error) (err error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != runtimeCrash {
			t.Fatalf("expected exact interruption, got %v", recovered)
		}
	}()
	err = start()
	return err
}
