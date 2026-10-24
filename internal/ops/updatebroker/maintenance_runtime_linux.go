//go:build linux

package updatebroker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const providerRestartReplyGrace = 2 * time.Second

var errMaintenanceRecovery = errors.New("provider maintenance requires recovery")

type maintenanceServiceControl interface {
	Ready(context.Context) error
	RestartPanel(context.Context) error
}

// The existing Root Update owner retains its checkpoint while only the Panel
// restarts. Its broker executable must be byte-identical in both generations;
// matching a version, provider label or pathname is not sufficient authority.
type nativeMaintenanceRuntime struct {
	host        *Host
	mu          sync.Mutex
	control     maintenanceServiceControl
	openRunning func() (*executableobject.Object, error)
	restartKey  string
	restarted   bool
	lastAttempt time.Time
}

func newNativeMaintenanceRuntime(host *Host) *nativeMaintenanceRuntime {
	runtime := &nativeMaintenanceRuntime{host: host, control: newReleaseServiceControl(),
		openRunning: func() (*executableobject.Object, error) {
			policy := sharedBrokerPolicy()
			policy.AllowSymlink = true // Kernel-selected object, never a Panel path.
			return executableobject.Open("/proc/self/exe", policy)
		}}
	host.maintenance = runtime
	return runtime
}

func sharedBrokerPolicy() executableobject.Policy {
	return executableobject.Policy{MaxBytes: 512 << 20, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, RequiredMode: 0o755, ForbiddenMode: 0o022, RequireTrustedAncestry: true,
		AllowRootOwnedStickyAncestry: true, RequireStablePath: true}
}

func (runtime *nativeMaintenanceRuntime) handler(handler broker.Handler) broker.Handler {
	return func(ctx context.Context, request broker.Request, peer broker.PeerIdentity) (any, error) {
		// Observe also participates: legacy observation can persist verification.
		// Waiting RPCs honor their existing request deadline; the watcher skips a
		// busy owner and therefore cannot obstruct broker shutdown.
		for !runtime.mu.TryLock() {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, broker.Failure(broker.CodeDeadline, "update owner admission expired")
			case <-timer.C:
			}
		}
		defer runtime.mu.Unlock()
		if ctx.Err() != nil {
			return nil, broker.Failure(broker.CodeDeadline, "update owner admission expired")
		}
		return handler(ctx, request, peer)
	}
}

func (runtime *nativeMaintenanceRuntime) admit(ctx context.Context, root string) error {
	if runtime.sharedBroker(root) != nil || runtime.control == nil || runtime.control.Ready(ctx) != nil {
		return errMaintenanceRecovery
	}
	return nil
}

func (runtime *nativeMaintenanceRuntime) sharedBroker(root string) error {
	if runtime.openRunning == nil {
		return errMaintenanceRecovery
	}
	running, err := runtime.openRunning()
	if err != nil || running == nil {
		return errMaintenanceRecovery
	}
	defer func() { _ = running.Close() }()
	target, err := executableobject.Open(filepath.Join(root, "solovey-privileged-broker"), sharedBrokerPolicy())
	if err != nil {
		return errMaintenanceRecovery
	}
	defer func() { _ = target.Close() }()
	before, after := running.Identity(), target.Identity()
	if !validDigest(before.Digest) || before.Digest != after.Digest || before.Size != after.Size ||
		running.Revalidate() != nil || target.Revalidate() != nil {
		return errMaintenanceRecovery
	}
	return nil
}

func (runtime *nativeMaintenanceRuntime) runningRelease() string {
	object, err := runtime.openRunning()
	if err != nil || object == nil {
		return ""
	}
	defer func() { _ = object.Close() }()
	path := object.Identity().ResolvedPath
	name := filepath.Base(filepath.Dir(path))
	if object.Revalidate() != nil || filepath.Base(path) != "solovey-privileged-broker" ||
		!releaseDirectoryPattern.MatchString(name) || filepath.Dir(filepath.Dir(path)) != filepath.Clean(runtime.host.ReleaseRoot) {
		return ""
	}
	return name
}

func (runtime *nativeMaintenanceRuntime) reconcile(ctx context.Context) error {
	if ctx == nil {
		return errMaintenanceRecovery
	}
	if ctx.Err() != nil || !runtime.mu.TryLock() {
		return ctx.Err()
	}
	defer runtime.mu.Unlock()
	state, err := runtime.host.loadState()
	if err != nil {
		return errMaintenanceRecovery
	}
	phase := state.ProviderMaintenance.Phase
	if phase != "ACTIVATED" && phase != "ROLLED_BACK" {
		return nil
	}
	now := runtime.host.Now()
	// UpdatedAt is not refreshed while ACTIVATED: observation and unsuccessful
	// health verification cannot extend the original activation deadline.
	if state.UpdatedAt <= 0 || now.Unix() < state.UpdatedAt {
		return errMaintenanceRecovery
	}
	elapsed := now.Sub(time.Unix(state.UpdatedAt, 0))
	if elapsed >= ProviderActivationHealthDeadline {
		if phase == "ACTIVATED" {
			return runtime.rollback(ctx, state, "provider_health_deadline")
		}
		return errMaintenanceRecovery
	}
	if elapsed < providerRestartReplyGrace {
		return nil
	}
	key := fmt.Sprintf("%s/%s/%s/%d", state.ProviderMaintenance.OperationID, phase, state.ActiveRelease, state.UpdatedAt)
	if runtime.restartKey != key {
		runtime.restartKey, runtime.restarted, runtime.lastAttempt = key, false, time.Time{}
	}
	if runtime.restarted || !runtime.lastAttempt.IsZero() && now.Sub(runtime.lastAttempt) < releaseControlTimeout {
		return nil
	}
	runtime.lastAttempt = now
	if runtime.admit(ctx, filepath.Join(runtime.host.ReleaseRoot, state.ActiveRelease)) == nil && runtime.control.RestartPanel(ctx) == nil {
		runtime.restarted = true
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if phase == "ACTIVATED" {
		return runtime.rollback(ctx, state, "provider_restart_failed")
	}
	return errMaintenanceRecovery
}

func (runtime *nativeMaintenanceRuntime) rollback(ctx context.Context, state diskState, reason string) error {
	proof := state.ProviderMaintenance
	identity, err := loadReleaseIdentity(filepath.Join(runtime.host.ReleaseRoot, proof.TargetRelease))
	if err != nil || !proof.matches(proof.OperationID, identity) {
		return errMaintenanceRecovery
	}
	_, err = runtime.host.rollbackCheckpoint(ctx, proof.OperationID, RollbackRequestV1{Release: identity,
		RollbackRef: SemanticReference("rollback", proof.OperationID, proof.ManifestDigest), ReasonCode: reason}, broker.PeerIdentity{})
	if err != nil {
		return errors.Join(errMaintenanceRecovery, err)
	}
	return nil
}
