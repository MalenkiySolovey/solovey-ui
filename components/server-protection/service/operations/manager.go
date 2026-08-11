// Package operations owns the persisted global lock and recovery state machine
// for dangerous server-protection operations. It never performs system changes.
// Native fallback lock order is fixed: process gate, persisted operation row,
// provider reservation, then the core mutation coordinator. Callers must not
// reverse it.
package operations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	StatePrepared          = "prepared"
	StateApplying          = "applying"
	StateHealth            = "health"
	StateHealthFailed      = "health_failed"
	StateRollingBack       = "rolling_back"
	StateLockSuspect       = "lock_suspect"
	StateApplied           = "applied"
	StateRolledBack        = "rolled_back"
	StateRollbackFailed    = "rollback_failed"
	StateReconcileRequired = "reconcile_required"
	StateAbandoned         = "abandoned"
	StateForceUnlocked     = "force_unlocked"
	StateForgotten         = "forgotten"
	StateCancelled         = "cancelled"

	KindFirewall       = "firewall"
	KindFronting       = "fronting"
	KindPortHandoff    = "port_handoff"
	KindNode           = "node"
	KindNativeFallback = "native_fallback"
	KindLocalProxy     = "local_proxy"
)

var (
	ErrConflict              = protectionrepository.ErrOperationConflict
	ErrFenced                = protectionrepository.ErrOperationFenced
	ErrRevisionConflict      = protectionrepository.ErrRevisionConflict
	ErrConfirmationRequired  = errors.New("explicit operation confirmation is required")
	ErrAuditUnavailable      = errors.New("operation audit recorder is unavailable")
	ErrCapabilityUnavailable = errors.New("system mutation capability is unavailable")
	ErrRecoveryUnavailable   = errors.New("automatic rollback capability is unavailable")
	globalProcessMutex       sync.Mutex
)

type Store interface {
	AcquireOperationLock(context.Context, protectionrepository.AcquireOperationLockInput) (protectionrepository.OperationLockModel, bool, error)
	OperationByIdempotencyKey(context.Context, string) (protectionrepository.OperationLockModel, error)
	OperationByID(context.Context, string) (protectionrepository.OperationLockModel, error)
	ListOperationLocks(context.Context, []string) ([]protectionrepository.OperationLockModel, error)
	UpdateOperationLockFenced(context.Context, protectionrepository.FencedOperationLockUpdate) (protectionrepository.OperationLockModel, error)
	HeartbeatOperationLock(context.Context, string, string, int, int, int64, int64) (protectionrepository.OperationLockModel, error)
	RecoverOperationLock(context.Context, protectionrepository.RecoveryOperationLockUpdate) (protectionrepository.OperationLockModel, error)
	ReclaimOperationLock(context.Context, protectionrepository.ReclaimOperationLockUpdate) (protectionrepository.OperationLockModel, error)
	ForceUnlockOperation(context.Context, string, int, int64) (protectionrepository.OperationLockModel, error)
	ForgetOperationState(context.Context, string, int, int64) (protectionrepository.OperationLockModel, error)
	MarkOperationRecovery(context.Context, string, int, int64, string) error
}

type PIDProbe interface {
	Alive(int) (bool, error)
}

type AuditEvent struct {
	Actor   string
	Event   string
	Details map[string]any
}

type Options struct {
	InstanceID     string
	PID            int
	Now            func() time.Time
	PIDProbe       PIDProbe
	HeartbeatEvery time.Duration
	RecoveryEvery  time.Duration
	Lease          time.Duration
	Audit          func(context.Context, AuditEvent) error
	Recovery       Recovery
}

type Recovery interface {
	HasMutationArtifact(context.Context, protectionrepository.OperationLockModel) (bool, error)
	AttemptRollback(context.Context, protectionrepository.OperationLockModel) error
	CreateBundle(context.Context, protectionrepository.OperationLockModel, string) error
}

type ReconcileDecision struct {
	State  string
	Reason string
}

// Reconciler owns exact restart classification for an operation kind whose
// recovery cannot be reduced to "artifact present means rollback". The
// manager supplies the process gate and a freshly reclaimed persisted fence.
type Reconciler interface {
	Reconcile(context.Context, protectionrepository.OperationLockModel) (ReconcileDecision, error)
}

type Manager struct {
	store Store
	opts  Options

	mu               sync.Mutex
	started          bool
	stopped          bool
	stop             context.CancelFunc
	done             chan struct{}
	active           *protectionrepository.OperationLockModel
	ownsGate         bool
	recoveredSuspect map[string]struct{}
	recoveries       map[string]Recovery
	reconcilers      map[string]Reconciler
}

type AcquireRequest struct {
	Kind           string `json:"kind"`
	ResourceID     string `json:"resourceId,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	Listen         string `json:"listen,omitempty"`
	Port           *int   `json:"port,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`
	PlanRevision   string `json:"planRevision,omitempty"`
	HelperRevision string `json:"helperRevision,omitempty"`
	Actor          string `json:"actor"`
	InitialState   string `json:"initialState,omitempty"`
}

type AcquireResult struct {
	Operation protectionrepository.OperationLockModel `json:"operation"`
	Joined    bool                                    `json:"joined"`
}

type ForceUnlockRequest struct {
	OperationID string
	Revision    int
	Actor       string
	Confirmed   bool
}

type PrepareRequest struct {
	Acquire      AcquireRequest
	PlanRevision string
	Confirmation string
}

type ConfirmActionRequest struct {
	OperationID  string
	Action       string
	Actor        string
	Confirmation string
}

type ForgetStateRequest struct {
	OperationID  string
	Revision     int
	Actor        string
	Confirmation string
}

type RecoveryResult struct {
	OperationID string `json:"operationId"`
	FromState   string `json:"fromState"`
	ToState     string `json:"toState"`
	Reason      string `json:"reason"`
}

func NewManager(store Store, options Options) *Manager {
	if options.InstanceID == "" {
		options.InstanceID = newID("instance")
	}
	if options.PID <= 0 {
		options.PID = os.Getpid()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.PIDProbe == nil {
		options.PIDProbe = systemPIDProbe{}
	}
	if options.HeartbeatEvery <= 0 {
		options.HeartbeatEvery = 2 * time.Second
	}
	if options.RecoveryEvery <= 0 {
		options.RecoveryEvery = 60 * time.Second
	}
	if options.Lease <= 0 {
		options.Lease = 2 * time.Minute
	}
	return &Manager{store: store, opts: options, recoveredSuspect: make(map[string]struct{}), recoveries: make(map[string]Recovery), reconcilers: make(map[string]Reconciler)}
}

func (m *Manager) InstanceID() string { return m.opts.InstanceID }

func (m *Manager) SetRecovery(recovery Recovery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		if m.opts.Recovery != nil {
			return nil
		}
		return errors.New("recovery backend cannot change while the runner is active")
	}
	m.opts.Recovery = recovery
	return nil
}

// SetRecoveryForKind installs a typed recovery backend for one operation
// family. It prevents port-handoff recovery from being routed through the
// already configured firewall backend.
func (m *Manager) SetRecoveryForKind(kind string, recovery Recovery) error {
	if !validKind(kind) || recovery == nil {
		return errors.New("valid operation kind and recovery backend are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		if m.recoveries[kind] != nil {
			return nil
		}
		return errors.New("kind recovery backend cannot change while the runner is active")
	}
	m.recoveries[kind] = recovery
	return nil
}

func (m *Manager) SetReconcilerForKind(kind string, reconciler Reconciler) error {
	if !validKind(kind) || reconciler == nil {
		return errors.New("valid operation kind and reconciler are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		if m.reconcilers[kind] != nil {
			return nil
		}
		return errors.New("kind reconciler cannot change while the runner is active")
	}
	m.reconcilers[kind] = reconciler
	return nil
}

func (m *Manager) reconcilerForKind(kind string) Reconciler {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reconcilers[kind]
}

func (m *Manager) recoveryForKind(kind string) Recovery {
	m.mu.Lock()
	defer m.mu.Unlock()
	if recovery := m.recoveries[kind]; recovery != nil {
		return recovery
	}
	return m.opts.Recovery
}

// ValidateHelperLock proves that a helper request is correlated with the
// current process gate and the persisted, fenced operation row. The helper
// client calls this immediately before every non-capability invocation.
func (m *Manager) ValidateHelperLock(ctx context.Context, operationID, instanceID, kind string, revision int) error {
	return m.validateHelperLock(ctx, operationID, instanceID, kind, revision, false)
}

// ValidateHelperReadLock permits only typed active verification while a
// reconciler owns a freshly reclaimed APPLIED or RECONCILE_REQUIRED row.
// Mutation verbs must continue to use ValidateHelperLock and remain fenced.
func (m *Manager) ValidateHelperReadLock(ctx context.Context, operationID, instanceID, kind string, revision int) error {
	return m.validateHelperLock(ctx, operationID, instanceID, kind, revision, true)
}

func (m *Manager) validateHelperLock(ctx context.Context, operationID, instanceID, kind string, revision int, allowTerminalRead bool) error {
	m.mu.Lock()
	active := m.active
	ownsGate := m.ownsGate
	stopped := m.stopped
	m.mu.Unlock()
	if stopped || !ownsGate || active == nil || active.OperationID != operationID ||
		active.LockedByInstanceID != instanceID || active.Kind != kind || active.Revision != revision {
		return ErrFenced
	}
	item, err := m.store.OperationByID(ctx, operationID)
	if err != nil {
		return ErrFenced
	}
	if item.LockedByPID == nil || *item.LockedByPID != m.opts.PID ||
		item.LockedByInstanceID != m.opts.InstanceID || item.LockedByInstanceID != instanceID ||
		item.Kind != kind || item.Revision != revision || item.State == StateLockSuspect ||
		isTerminal(item.State) && (!allowTerminalRead || item.State != StateApplied && item.State != StateReconcileRequired) {
		return ErrFenced
	}
	return nil
}

// ValidateHelperListener binds a helper probe to the exact socket recorded in
// the active fenced port-handoff lock. It prevents the typed probe from being
// repurposed as an arbitrary network scanner.
func (m *Manager) ValidateHelperListener(ctx context.Context, operationID, instanceID string, revision int, network, address string, port, expectedPID int) error {
	if network != "tcp" {
		return ErrFenced
	}
	if err := m.ValidateHelperLock(ctx, operationID, instanceID, KindPortHandoff, revision); err != nil {
		return err
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active == nil || active.LockedByPID == nil || *active.LockedByPID != expectedPID || active.Protocol != network || canonicalFenceAddress(active.Listen) != canonicalFenceAddress(address) || active.Port == nil || *active.Port != port {
		return ErrFenced
	}
	return nil
}

func canonicalFenceAddress(value string) string {
	address, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(value), "[]"))
	if err != nil {
		return strings.TrimSpace(value)
	}
	return address.Unmap().String()
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if _, err := m.Recover(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		cancel()
		return nil
	}
	m.started = true
	m.stopped = false
	m.stop = cancel
	m.done = make(chan struct{})
	done := m.done
	m.mu.Unlock()
	go m.run(runCtx, done)
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.stopped = true
		m.active = nil
		clear(m.recoveredSuspect)
		m.releaseGateLocked()
		m.mu.Unlock()
		return nil
	}
	stop, done := m.stop, m.done
	m.started = false
	m.stopped = true
	m.stop = nil
	m.done = nil
	m.active = nil
	clear(m.recoveredSuspect)
	m.releaseGateLocked()
	m.mu.Unlock()
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Acquire(ctx context.Context, request AcquireRequest) (AcquireResult, error) {
	if m.isStopped() {
		return AcquireResult{}, ErrFenced
	}
	request.Kind = strings.TrimSpace(request.Kind)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Actor = strings.TrimSpace(request.Actor)
	if !validKind(request.Kind) {
		return AcquireResult{}, fmt.Errorf("invalid operation kind %q", request.Kind)
	}
	if request.IdempotencyKey == "" {
		return AcquireResult{}, errors.New("idempotency key is required")
	}
	if request.Actor == "" {
		return AcquireResult{}, errors.New("actor is required")
	}
	if request.Port != nil && (*request.Port < 1 || *request.Port > 65535) {
		return AcquireResult{}, errors.New("port must be between 1 and 65535")
	}
	if existing, err := m.store.OperationByIdempotencyKey(ctx, request.IdempotencyKey); err == nil {
		return AcquireResult{Operation: existing, Joined: true}, nil
	} else if !errors.Is(err, protectionrepository.ErrRecordNotFound) {
		return AcquireResult{}, err
	}

	if !m.tryAcquireGate() {
		return AcquireResult{}, ErrConflict
	}
	state := request.InitialState
	if state == "" {
		state = StatePrepared
	}
	if state != StatePrepared && state != StateApplying && state != StateRollingBack {
		m.releaseGate()
		return AcquireResult{}, fmt.Errorf("invalid initial operation state %q", state)
	}
	now := m.opts.Now()
	item, joined, err := m.store.AcquireOperationLock(ctx, protectionrepository.AcquireOperationLockInput{
		OperationID: newID("operation"), Kind: request.Kind, ResourceID: strings.TrimSpace(request.ResourceID),
		Protocol: strings.TrimSpace(request.Protocol), Listen: strings.TrimSpace(request.Listen), Port: request.Port,
		State: state, IdempotencyKey: request.IdempotencyKey, LockedByPID: m.opts.PID,
		PlanRevision:       strings.TrimSpace(request.PlanRevision),
		HelperRevision:     strings.TrimSpace(request.HelperRevision),
		LockedByInstanceID: m.opts.InstanceID, Actor: request.Actor, Now: now.Unix(), ExpiresAt: now.Add(m.opts.Lease).Unix(),
	})
	if err != nil {
		m.releaseGate()
		return AcquireResult{}, err
	}
	if joined {
		m.releaseGate()
		return AcquireResult{Operation: item, Joined: true}, nil
	}
	m.mu.Lock()
	m.active = &item
	m.mu.Unlock()
	return AcquireResult{Operation: item}, nil
}

func (m *Manager) Transition(ctx context.Context, operationID string, revision int, toState string) (protectionrepository.OperationLockModel, error) {
	return m.transition(ctx, operationID, revision, toState, nil)
}

// TransitionWithBinding persists an opaque, non-secret request binding in the
// existing operation fence as part of the same state CAS.
func (m *Manager) TransitionWithBinding(ctx context.Context, operationID string, revision int, toState, binding string) (protectionrepository.OperationLockModel, error) {
	return m.transition(ctx, operationID, revision, toState, &binding)
}

func (m *Manager) transition(ctx context.Context, operationID string, revision int, toState string, binding *string) (protectionrepository.OperationLockModel, error) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return protectionrepository.OperationLockModel{}, ErrFenced
	}
	active := m.active
	m.mu.Unlock()
	if active == nil || active.OperationID != operationID {
		return protectionrepository.OperationLockModel{}, ErrFenced
	}
	fromStates := allowedFromStates(toState)
	if len(fromStates) == 0 {
		return protectionrepository.OperationLockModel{}, fmt.Errorf("invalid terminal or non-terminal transition to %q", toState)
	}
	now := m.opts.Now()
	item, err := m.store.UpdateOperationLockFenced(ctx, protectionrepository.FencedOperationLockUpdate{
		OperationID: operationID, Revision: revision, InstanceID: m.opts.InstanceID, PID: m.opts.PID,
		FromStates: fromStates, ToState: toState, HelperRevision: binding, Now: now.Unix(), ExpiresAt: now.Add(m.opts.Lease).Unix(),
	})
	if err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	m.mu.Lock()
	if isTerminal(toState) {
		m.active = nil
		m.releaseGateLocked()
	} else {
		m.active = &item
	}
	m.mu.Unlock()
	return item, nil
}

// BeginRollback fences and reclaims a completed applied operation before a
// manual rollback. The new owner/revision are persisted before helper use.
func (m *Manager) BeginRollback(ctx context.Context, operationID string, revision int) (protectionrepository.OperationLockModel, error) {
	return m.beginRollback(ctx, operationID, revision, nil)
}

// BeginRollbackWithBinding reclaims an applied operation and persists the
// opaque rollback request binding in the same CAS.
func (m *Manager) BeginRollbackWithBinding(ctx context.Context, operationID string, revision int, binding string) (protectionrepository.OperationLockModel, error) {
	return m.beginRollback(ctx, operationID, revision, &binding)
}

func (m *Manager) beginRollback(ctx context.Context, operationID string, revision int, binding *string) (protectionrepository.OperationLockModel, error) {
	if m.isStopped() || !m.tryAcquireGate() {
		return protectionrepository.OperationLockModel{}, ErrConflict
	}
	now := m.opts.Now()
	item, err := m.store.ReclaimOperationLock(ctx, protectionrepository.ReclaimOperationLockUpdate{
		OperationID: operationID, Revision: revision, InstanceID: m.opts.InstanceID, PID: m.opts.PID,
		FromState: StateApplied, ToState: StateRollingBack, HelperRevision: binding, Now: now.Unix(), ExpiresAt: now.Add(m.opts.Lease).Unix(),
	})
	if err != nil {
		m.releaseGate()
		return protectionrepository.OperationLockModel{}, err
	}
	m.mu.Lock()
	m.active = &item
	m.mu.Unlock()
	return item, nil
}

func (m *Manager) Heartbeat(ctx context.Context, operationID string, revision int) (protectionrepository.OperationLockModel, error) {
	if m.isStopped() {
		return protectionrepository.OperationLockModel{}, ErrFenced
	}
	now := m.opts.Now()
	item, err := m.store.HeartbeatOperationLock(ctx, operationID, m.opts.InstanceID, m.opts.PID, revision, now.Unix(), now.Add(m.opts.Lease).Unix())
	if err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	m.mu.Lock()
	if m.active != nil && m.active.OperationID == operationID {
		m.active = &item
	}
	m.mu.Unlock()
	return item, nil
}

func (m *Manager) List(ctx context.Context) ([]protectionrepository.OperationLockModel, error) {
	return m.store.ListOperationLocks(ctx, nil)
}

func (m *Manager) Prepare(ctx context.Context, request PrepareRequest) (AcquireResult, error) {
	if request.Confirmation != "PREPARE SERVER PROTECTION "+request.PlanRevision || strings.TrimSpace(request.PlanRevision) == "" {
		return AcquireResult{}, ErrConfirmationRequired
	}
	if err := m.auditAction(ctx, request.Acquire.Actor, "server_protection.prepare", request.Acquire.IdempotencyKey, "attempt"); err != nil {
		return AcquireResult{}, err
	}
	request.Acquire.InitialState = StatePrepared
	request.Acquire.PlanRevision = request.PlanRevision
	return m.Acquire(ctx, request.Acquire)
}

func (m *Manager) ConfirmUnavailableAction(ctx context.Context, request ConfirmActionRequest) error {
	action := strings.TrimSpace(request.Action)
	if action != "apply" && action != "rollback" {
		return errors.New("unsupported confirmed action")
	}
	if request.Confirmation != strings.ToUpper(action)+" SERVER PROTECTION "+request.OperationID {
		return ErrConfirmationRequired
	}
	if _, err := m.store.OperationByID(ctx, request.OperationID); err != nil {
		return err
	}
	if err := m.auditAction(ctx, request.Actor, "server_protection."+action, request.OperationID, "missing_capability"); err != nil {
		return err
	}
	return ErrCapabilityUnavailable
}

func (m *Manager) ForgetState(ctx context.Context, request ForgetStateRequest) (protectionrepository.OperationLockModel, error) {
	if request.Confirmation != "FORGET_SERVER_PROTECTION_STATE" {
		return protectionrepository.OperationLockModel{}, ErrConfirmationRequired
	}
	if err := m.auditAction(ctx, request.Actor, "server_protection.forget_state", request.OperationID, "attempt"); err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	return m.store.ForgetOperationState(ctx, request.OperationID, request.Revision, m.opts.Now().Unix())
}

func (m *Manager) ForceUnlock(ctx context.Context, request ForceUnlockRequest) (protectionrepository.OperationLockModel, error) {
	if !request.Confirmed {
		return protectionrepository.OperationLockModel{}, ErrConfirmationRequired
	}
	if strings.TrimSpace(request.Actor) == "" {
		return protectionrepository.OperationLockModel{}, errors.New("actor is required")
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return protectionrepository.OperationLockModel{}, ErrFenced
	}
	allowed := m.active != nil && m.active.OperationID == request.OperationID
	if _, ok := m.recoveredSuspect[request.OperationID]; ok {
		allowed = true
	}
	m.mu.Unlock()
	if !allowed {
		return protectionrepository.OperationLockModel{}, ErrFenced
	}
	if m.opts.Audit == nil {
		return protectionrepository.OperationLockModel{}, ErrAuditUnavailable
	}
	if err := m.opts.Audit(ctx, AuditEvent{Actor: request.Actor, Event: "server_protection.force_unlock", Details: map[string]any{
		"operation_id": request.OperationID, "previous_revision": request.Revision, "phase": "attempt",
	}}); err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	item, err := m.store.ForceUnlockOperation(ctx, request.OperationID, request.Revision, m.opts.Now().Unix())
	if err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	m.mu.Lock()
	if m.active != nil && m.active.OperationID == item.OperationID {
		m.active = nil
	}
	delete(m.recoveredSuspect, item.OperationID)
	m.releaseGateLocked()
	m.mu.Unlock()
	return item, nil
}

func (m *Manager) Recover(ctx context.Context) ([]RecoveryResult, error) {
	all, err := m.store.ListOperationLocks(ctx, nil)
	if err != nil {
		return nil, err
	}
	items := make([]protectionrepository.OperationLockModel, 0, len(all))
	for _, item := range all {
		if containsState(protectionrepository.NonTerminalOperationLockStates(), item.State) ||
			((item.State == StateApplied || item.State == StateReconcileRequired) && m.reconcilerForKind(item.Kind) != nil) {
			items = append(items, item)
		}
	}
	now := m.opts.Now()
	results := make([]RecoveryResult, 0, len(items))
	for _, item := range items {
		if reconciler := m.reconcilerForKind(item.Kind); reconciler != nil {
			m.mu.Lock()
			ownedHere := m.active != nil && m.active.OperationID == item.OperationID && item.LockedByInstanceID == m.opts.InstanceID && item.LockedByPID != nil && *item.LockedByPID == m.opts.PID
			m.mu.Unlock()
			if ownedHere {
				continue
			}
			updated, reason, reconcileErr := m.reconcileOwnedKind(ctx, item, reconciler)
			if reconcileErr != nil {
				return results, reconcileErr
			}
			results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: item.State, ToState: updated.State, Reason: reason})
			continue
		}
		m.tryAcquireGate()
		if item.LockedByInstanceID == m.opts.InstanceID && item.LockedByPID != nil && *item.LockedByPID == m.opts.PID {
			if item.State == StatePrepared && now.Unix() > item.ExpiresAt {
				target, reason := StateAbandoned, "prepared_expired"
				if item.Kind == KindFronting {
					target, reason = StateCancelled, "fronting_cancelled_before_mutation"
				}
				updated, updateErr := m.Transition(ctx, item.OperationID, item.Revision, target)
				if updateErr != nil {
					return results, updateErr
				}
				results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: item.State, ToState: updated.State, Reason: reason})
			}
			continue
		}
		alive, probeErr := true, errors.New("pid is unavailable")
		if item.LockedByPID != nil {
			alive, probeErr = m.opts.PIDProbe.Alive(*item.LockedByPID)
		}
		toState, reason := StateLockSuspect, "pid_liveness_unknown"
		if probeErr == nil && alive {
			reason = "pid_alive_or_reused"
		} else if probeErr == nil && !alive && now.Unix() > item.ExpiresAt {
			toState, reason = m.recoveryTarget(ctx, item)
		} else if probeErr == nil && !alive {
			reason = "heartbeat_not_expired"
		}
		if item.State == toState {
			if toState == StateRollingBack {
				results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: item.State, ToState: item.State, Reason: reason})
				final, finalReason, finalErr := m.recoverRollingBack(ctx, item)
				if finalErr != nil {
					return results, finalErr
				}
				results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: item.State, ToState: final.State, Reason: finalReason})
				continue
			}
			if toState == StateLockSuspect {
				m.mu.Lock()
				m.recoveredSuspect[item.OperationID] = struct{}{}
				m.mu.Unlock()
			}
			results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: item.State, ToState: item.State, Reason: reason})
			continue
		}
		updated, updateErr := m.store.RecoverOperationLock(ctx, protectionrepository.RecoveryOperationLockUpdate{
			OperationID: item.OperationID, ExpectedRevision: item.Revision,
			FromStates: protectionrepository.NonTerminalOperationLockStates(), ToState: toState, Now: now.Unix(),
		})
		if updateErr != nil {
			return results, updateErr
		}
		results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: item.State, ToState: updated.State, Reason: reason})
		if updated.State == StateRollingBack {
			final, finalReason, finalErr := m.recoverRollingBack(ctx, updated)
			if finalErr != nil {
				return results, finalErr
			}
			results = append(results, RecoveryResult{OperationID: item.OperationID, FromState: updated.State, ToState: final.State, Reason: finalReason})
		}
		if updated.State == StateLockSuspect {
			m.mu.Lock()
			m.recoveredSuspect[item.OperationID] = struct{}{}
			m.mu.Unlock()
		}
	}
	remaining, err := m.store.ListOperationLocks(ctx, protectionrepository.NonTerminalOperationLockStates())
	if err != nil {
		return results, err
	}
	if len(remaining) == 0 {
		m.releaseGate()
	}
	return results, nil
}

func (m *Manager) reconcileOwnedKind(ctx context.Context, item protectionrepository.OperationLockModel, reconciler Reconciler) (protectionrepository.OperationLockModel, string, error) {
	if err := ctx.Err(); err != nil {
		return protectionrepository.OperationLockModel{}, "reconcile_cancelled", err
	}
	if !m.tryAcquireGate() {
		return protectionrepository.OperationLockModel{}, "reconcile_gate_conflict", ErrConflict
	}
	now := m.opts.Now()
	claimed, err := m.store.ReclaimOperationLock(ctx, protectionrepository.ReclaimOperationLockUpdate{
		OperationID: item.OperationID, Revision: item.Revision, InstanceID: m.opts.InstanceID, PID: m.opts.PID,
		FromState: item.State, ToState: item.State, Now: now.Unix(), ExpiresAt: now.Add(m.opts.Lease).Unix(),
	})
	if err != nil {
		m.releaseGate()
		return protectionrepository.OperationLockModel{}, "reconcile_fencing_failed", err
	}
	m.mu.Lock()
	m.active = &claimed
	m.mu.Unlock()
	decision, err := reconciler.Reconcile(ctx, claimed)
	if err != nil {
		m.mu.Lock()
		m.active = nil
		m.releaseGateLocked()
		m.mu.Unlock()
		return protectionrepository.OperationLockModel{}, "reconcile_backend_failed", err
	}
	if decision.Reason == "" {
		decision.Reason = "reconciled"
	}
	if !reconcileTerminalState(decision.State) {
		m.mu.Lock()
		m.active = nil
		m.releaseGateLocked()
		m.mu.Unlock()
		return protectionrepository.OperationLockModel{}, "reconcile_decision_invalid", errors.New("kind reconciler returned an invalid terminal state")
	}
	updated, err := m.store.RecoverOperationLock(ctx, protectionrepository.RecoveryOperationLockUpdate{
		OperationID: claimed.OperationID, ExpectedRevision: claimed.Revision, FromStates: []string{claimed.State}, ToState: decision.State, Now: m.opts.Now().Unix(),
	})
	m.mu.Lock()
	m.active = nil
	m.releaseGateLocked()
	m.mu.Unlock()
	return updated, decision.Reason, err
}

// recoverRollingBack takes a fresh persisted owner/PID fence and the global
// process gate before the recovery backend can reach the privileged helper.
func (m *Manager) recoverRollingBack(ctx context.Context, item protectionrepository.OperationLockModel) (protectionrepository.OperationLockModel, string, error) {
	if !m.tryAcquireGate() {
		return protectionrepository.OperationLockModel{}, "recovery_gate_conflict", ErrConflict
	}
	now := m.opts.Now()
	claimed, err := m.store.ReclaimOperationLock(ctx, protectionrepository.ReclaimOperationLockUpdate{
		OperationID: item.OperationID, Revision: item.Revision, InstanceID: m.opts.InstanceID, PID: m.opts.PID,
		FromState: StateRollingBack, ToState: StateRollingBack, Now: now.Unix(), ExpiresAt: now.Add(m.opts.Lease).Unix(),
	})
	if err != nil {
		m.releaseGate()
		return protectionrepository.OperationLockModel{}, "recovery_fencing_failed", err
	}
	m.mu.Lock()
	m.active = &claimed
	m.mu.Unlock()
	finalState, reason := m.attemptRecoveryRollback(ctx, claimed)
	final, err := m.Transition(ctx, claimed.OperationID, claimed.Revision, finalState)
	if err != nil {
		m.releaseGate()
	}
	return final, reason, err
}

func (m *Manager) recoveryTarget(ctx context.Context, item protectionrepository.OperationLockModel) (string, string) {
	recovery := m.recoveryForKind(item.Kind)
	if item.State == StatePrepared {
		if item.Kind == KindFronting {
			return StateCancelled, "fronting_cancelled_before_mutation"
		}
		return StateAbandoned, "prepared_expired"
	}
	if item.State == StateLockSuspect {
		return StateLockSuspect, "force_unlock_required"
	}
	if item.RecoveryAttempts > 0 {
		if recovery != nil {
			if err := recovery.CreateBundle(ctx, item, StateRollbackFailed); err != nil {
				return StateLockSuspect, "recovery_bundle_failed"
			}
		}
		return StateRollbackFailed, "automatic_rollback_already_attempted"
	}
	if recovery == nil {
		return StateAbandoned, "expired_and_pid_absent"
	}
	hasArtifact, err := recovery.HasMutationArtifact(ctx, item)
	if err != nil {
		_ = m.store.MarkOperationRecovery(ctx, item.OperationID, item.RecoveryAttempts+1, m.opts.Now().Unix(), "artifact_integrity_failed")
		if bundleErr := recovery.CreateBundle(ctx, item, StateRollbackFailed); bundleErr != nil {
			return StateLockSuspect, "recovery_bundle_failed"
		}
		return StateRollbackFailed, "artifact_integrity_failed"
	}
	if !hasArtifact {
		if item.Kind == KindFronting {
			return StateCancelled, "fronting_cancelled_before_mutation"
		}
		return StateAbandoned, "mutation_artifact_absent"
	}
	return StateRollingBack, "mutation_artifact_present"
}

func (m *Manager) attemptRecoveryRollback(ctx context.Context, item protectionrepository.OperationLockModel) (string, string) {
	at := m.opts.Now().Unix()
	recovery := m.recoveryForKind(item.Kind)
	if recovery == nil {
		_ = m.store.MarkOperationRecovery(ctx, item.OperationID, item.RecoveryAttempts+1, at, "rollback_capability_unavailable")
		return StateLockSuspect, "rollback_capability_unavailable"
	}
	err := recovery.AttemptRollback(ctx, item)
	if err == nil {
		_ = m.store.MarkOperationRecovery(ctx, item.OperationID, item.RecoveryAttempts+1, at, "")
		return StateRolledBack, "automatic_rollback_completed"
	}
	_ = m.store.MarkOperationRecovery(ctx, item.OperationID, item.RecoveryAttempts+1, at, "rollback_failed")
	if err := recovery.CreateBundle(ctx, item, StateRollbackFailed); err != nil {
		return StateLockSuspect, "recovery_bundle_failed"
	}
	return StateRollbackFailed, "automatic_rollback_failed"
}

func (m *Manager) auditAction(ctx context.Context, actor, event, operationID, phase string) error {
	if strings.TrimSpace(actor) == "" {
		return errors.New("actor is required")
	}
	if m.opts.Audit == nil {
		return ErrAuditUnavailable
	}
	return m.opts.Audit(ctx, AuditEvent{Actor: actor, Event: event, Details: map[string]any{
		"operation_id": operationID, "phase": phase,
	}})
}

func (m *Manager) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	heartbeat := time.NewTicker(m.opts.HeartbeatEvery)
	recovery := time.NewTicker(m.opts.RecoveryEvery)
	defer heartbeat.Stop()
	defer recovery.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			m.mu.Lock()
			active := m.active
			m.mu.Unlock()
			if active != nil && active.State != StatePrepared {
				_, _ = m.Heartbeat(ctx, active.OperationID, active.Revision)
			}
		case <-recovery.C:
			_, _ = m.Recover(ctx)
		}
	}
}

func (m *Manager) tryAcquireGate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ownsGate {
		return true
	}
	if globalProcessMutex.TryLock() {
		m.ownsGate = true
		return true
	}
	return false
}

func (m *Manager) releaseGate() {
	m.mu.Lock()
	m.releaseGateLocked()
	m.mu.Unlock()
}

func (m *Manager) releaseGateLocked() {
	if !m.ownsGate {
		return
	}
	globalProcessMutex.Unlock()
	m.ownsGate = false
}

func (m *Manager) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

func newID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}

func validKind(kind string) bool {
	return kind == KindFirewall || kind == KindFronting || kind == KindPortHandoff || kind == KindNode ||
		kind == KindNativeFallback || kind == KindLocalProxy
}

func isTerminal(state string) bool {
	switch state {
	case StateApplied, StateRolledBack, StateRollbackFailed, StateReconcileRequired, StateAbandoned, StateForceUnlocked, StateForgotten, StateCancelled:
		return true
	default:
		return false
	}
}

func allowedFromStates(to string) []string {
	switch to {
	case StateApplying:
		return []string{StatePrepared}
	case StateHealth:
		return []string{StateApplying}
	case StateApplied, StateHealthFailed:
		return []string{StateHealth, StateApplying}
	case StateRollingBack:
		return []string{StateApplying, StateHealth, StateHealthFailed}
	case StateRolledBack, StateRollbackFailed:
		return []string{StateRollingBack, StateHealthFailed}
	case StateReconcileRequired:
		return []string{StatePrepared, StateApplying, StateHealth, StateHealthFailed, StateRollingBack, StateApplied}
	case StateLockSuspect:
		return []string{StatePrepared, StateApplying, StateHealthFailed, StateRollingBack}
	case StateAbandoned:
		return []string{StatePrepared, StateApplying, StateHealthFailed, StateRollingBack, StateLockSuspect}
	case StateCancelled:
		// Applying may still be before the typed mutation boundary. The owning
		// workflow must prove that fact from its durable checkpoint before using
		// this transition; after a switch it must roll back instead.
		return []string{StatePrepared, StateApplying}
	default:
		return nil
	}
}

func reconcileTerminalState(state string) bool {
	switch state {
	case StateApplied, StateRolledBack, StateRollbackFailed, StateReconcileRequired, StateCancelled:
		return true
	default:
		return false
	}
}

func containsState(states []string, state string) bool {
	for _, candidate := range states {
		if candidate == state {
			return true
		}
	}
	return false
}
