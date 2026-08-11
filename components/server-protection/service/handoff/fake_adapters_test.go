package handoff

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Mock adapters record typed calls for normal CI; none creates a listener,
// writes inbound JSON or restarts a service.
type MockInbound struct {
	mu                sync.Mutex
	Calls             []string
	Err               map[string]error
	OnCall            map[string]func()
	ApplyRestarted    bool
	RollbackRestarted bool
}

func (m *MockInbound) call(name string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, name)
	err, hook := m.Err[name], m.OnCall[name]
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}
func (m *MockInbound) Prepare(context.Context, OwnerSnapshot, Fence) error { return m.call("prepare") }
func (m *MockInbound) AbortPrepare(context.Context, OwnerSnapshot, Fence) error {
	return m.call("abort_prepare")
}
func (m *MockInbound) Apply(context.Context, OwnerSnapshot, Fence) (CoreMutationResult, error) {
	return CoreMutationResult{Restarted: m.ApplyRestarted}, m.call("apply")
}
func (m *MockInbound) Rollback(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) (CoreMutationResult, error) {
	return CoreMutationResult{Restarted: m.RollbackRestarted}, m.call("rollback")
}

type MockRestart struct {
	mu    sync.Mutex
	Calls int
	Err   error
}

func (m *MockRestart) Restart(context.Context, Fence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	return m.Err
}

type MockFallback struct {
	mu     sync.Mutex
	Calls  []string
	Err    map[string]error
	OnCall map[string]func()
}

func (m *MockFallback) call(name string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, name)
	err, hook := m.Err[name], m.OnCall[name]
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}
func (m *MockFallback) Prepare(context.Context, OwnerSnapshot, Fence) error { return m.call("prepare") }
func (m *MockFallback) AbortPrepare(context.Context, OwnerSnapshot, Fence) error {
	return m.call("abort_prepare")
}
func (m *MockFallback) Apply(context.Context, OwnerSnapshot, Fence) error { return m.call("apply") }
func (m *MockFallback) Rollback(context.Context, OwnerSnapshot, Fence) error {
	return m.call("rollback")
}

type MockOwnership struct {
	mu           sync.Mutex
	CurrentOwner OwnerSnapshot
	Owners       []OwnerSnapshot
	Missing      bool
	Err          error
}

func (m *MockOwnership) Current(context.Context, string, string, int) (OwnerSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return OwnerSnapshot{}, m.Err
	}
	if m.Missing {
		return OwnerSnapshot{}, errors.New("owner missing")
	}
	return cloneSnapshot(m.CurrentOwner), nil
}
func (m *MockOwnership) Manifest(context.Context) ([]OwnerSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]OwnerSnapshot, 0, len(m.Owners))
	for _, value := range m.Owners {
		out = append(out, cloneSnapshot(value))
	}
	return out, nil
}

type MockHealth struct {
	Results  []HealthResult
	Sequence [][]HealthResult
	Err      error
	Calls    int
}

func (m *MockHealth) Check(_ context.Context, targets []HealthTarget) ([]HealthResult, error) {
	m.Calls++
	if len(m.Sequence) >= m.Calls {
		return append([]HealthResult(nil), m.Sequence[m.Calls-1]...), m.Err
	}
	if m.Results != nil {
		return append([]HealthResult(nil), m.Results...), m.Err
	}
	out := make([]HealthResult, 0, len(targets))
	for _, target := range targets {
		out = append(out, HealthResult{Target: target, OK: true, Fact: "mock_ok"})
	}
	return out, m.Err
}

type MockHelper struct {
	Value Capabilities
	Err   error
}

func (m MockHelper) Capabilities(context.Context) (Capabilities, error) {
	if m.Err != nil {
		return Capabilities{}, m.Err
	}
	return m.Value, nil
}

type MockRecoveryUX struct {
	Calls []RecoveryFacts
	Err   error
}

type MockSnapshot struct {
	mu       sync.Mutex
	Calls    []string
	Err      map[string]error
	Mutation bool
}

func (m *MockSnapshot) call(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, name)
	if name == "mark_mutation" && m.Err[name] == nil {
		m.Mutation = true
	}
	return m.Err[name]
}
func (m *MockSnapshot) Checkpoint(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) error {
	return m.call("checkpoint")
}
func (m *MockSnapshot) MarkMutation(context.Context, Fence) error { return m.call("mark_mutation") }
func (m *MockSnapshot) HasMutation(context.Context, Fence) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Mutation, m.Err["has_mutation"]
}

type MockListener struct {
	mu    sync.Mutex
	Calls []string
	Err   map[string]error
}

func (m *MockListener) Verify(_ context.Context, owner OwnerSnapshot, _ Fence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, owner.ResourceID)
	return m.Err[owner.ResourceID]
}

func (m *MockRecoveryUX) Record(_ context.Context, facts RecoveryFacts) error {
	m.Calls = append(m.Calls, facts)
	return m.Err
}

func DefaultMockCapabilities() Capabilities {
	return Capabilities{Revision: "mock-v1", ProxyProtocol: true, InboundDraft: true, SingBoxRestart: true, ListenerOwnership: true, FallbackTarget: true, Health: true, ExactListener: true}
}
func (m *MockOwnership) SetCurrent(value OwnerSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CurrentOwner = cloneSnapshot(value)
}
func (m *MockOwnership) AddOwner(value OwnerSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Owners = append(m.Owners, cloneSnapshot(value))
}
func (m *MockInbound) RequireNoCalls() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) != 0 {
		return fmt.Errorf("unexpected inbound calls: %v", m.Calls)
	}
	return nil
}
