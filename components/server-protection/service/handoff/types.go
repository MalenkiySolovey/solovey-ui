// Package handoff coordinates persisted, reversible listener ownership transfer.
package handoff

import (
	"context"
	"errors"
	"sync"
	"time"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// ContractRevision identifies the production handoff workflow contract.
const ContractRevision = "port-handoff-v1"

var (
	ErrInvalidPlan      = errors.New("invalid port handoff plan")
	ErrCollision        = errors.New("listener ownership collision")
	ErrRevisionConflict = errors.New("owner resource/config revision changed")
	ErrOwnerDisappeared = errors.New("previous port owner disappeared")
	ErrWildcardConfirm  = errors.New("wildcard listener requires advanced confirmation")
	ErrACMEConflict     = errors.New("ACME renewal route conflicts with the handoff")
	ErrProtocol         = errors.New("unsupported port handoff protocol")
	ErrProxyCapability  = errors.New("PROXY protocol capability is unavailable")
	ErrCriticalOwner    = errors.New("critical listener owner cannot be handed off")
	ErrExactListener    = errors.New("real handoff requires an exact listener address")
	ErrHealth           = errors.New("port handoff health checks failed")
	ErrListenerVerify   = errors.New("exact listener verification failed")
	ErrRollbackVerify   = errors.New("restored owner verification failed")
	ErrCancelled        = errors.New("port handoff cancelled")
	ErrServiceDisabled  = errors.New("port handoff service is not initialized")
)

const (
	StatePrepared       = protectionoperations.StatePrepared
	StateApplying       = protectionoperations.StateApplying
	StateHealth         = protectionoperations.StateHealth
	StateHealthFailed   = protectionoperations.StateHealthFailed
	StateRollingBack    = protectionoperations.StateRollingBack
	StateApplied        = protectionoperations.StateApplied
	StateRolledBack     = protectionoperations.StateRolledBack
	StateRollbackFailed = protectionoperations.StateRollbackFailed
	StateAbandoned      = protectionoperations.StateAbandoned
	StateCancelled      = protectionoperations.StateCancelled
)

// OwnerSnapshot is copied into both operation sides and never mutated. It is a
// listener manifest, not an inbound JSON document: no adapter receives or
// edits arbitrary core configuration.
type OwnerSnapshot struct {
	ResourceID       string   `json:"resourceId"`
	Owner            string   `json:"owner"`
	Kind             string   `json:"kind"`
	Protocol         string   `json:"protocol"`
	Listen           string   `json:"listen"`
	Port             int      `json:"port"`
	ResourceRevision string   `json:"resourceRevision"`
	ConfigRevision   string   `json:"configRevision"`
	Fingerprint      string   `json:"fingerprint"`
	ReservedRoutes   []string `json:"reservedRoutes,omitempty"`
	ACMERenewal      bool     `json:"acmeRenewal"`
	ProxyProtocol    bool     `json:"proxyProtocol"`
	Profile          Profile  `json:"profile"`
	ExpiresAt        int64    `json:"expiresAt"`
}

type Profile struct {
	Protocol       string            `json:"protocol"`
	Security       string            `json:"security"`
	HandshakeHost  string            `json:"handshakeHost,omitempty"`
	FallbackListen string            `json:"fallbackListen,omitempty"`
	FallbackPort   int               `json:"fallbackPort,omitempty"`
	StrictSNI      bool              `json:"strictSni"`
	CertificateRef string            `json:"certificateRef,omitempty"`
	ALPNFallbacks  map[string]string `json:"alpnFallbacks,omitempty"`
}

type HealthTarget struct {
	ResourceID string `json:"resourceId"`
	Check      string `json:"check"`
}

type Plan struct {
	PlanRevision      string         `json:"planRevision"`
	IdempotencyKey    string         `json:"idempotencyKey"`
	Actor             string         `json:"actor"`
	Previous          OwnerSnapshot  `json:"previous"`
	Next              OwnerSnapshot  `json:"next"`
	HealthTargets     []HealthTarget `json:"healthTargets"`
	AdvancedConfirmed bool           `json:"advancedConfirmed"`
	AdvancedPhrase    string         `json:"advancedPhrase,omitempty"`
}

type HealthResult struct {
	Target HealthTarget `json:"target"`
	OK     bool         `json:"ok"`
	Fact   string       `json:"fact"`
}

type Capabilities struct {
	Revision          string `json:"revision"`
	ProxyProtocol     bool   `json:"proxyProtocol"`
	InboundDraft      bool   `json:"inboundDraft"`
	SingBoxRestart    bool   `json:"singBoxRestart"`
	ListenerOwnership bool   `json:"listenerOwnership"`
	FallbackTarget    bool   `json:"fallbackTarget"`
	Health            bool   `json:"health"`
	ExactListener     bool   `json:"exactListener"`
}

type Fence struct {
	OperationID string
	Revision    int
	InstanceID  string
	PID         int
}

type InboundDraftAdapter interface {
	Prepare(context.Context, OwnerSnapshot, Fence) error
	AbortPrepare(context.Context, OwnerSnapshot, Fence) error
	Apply(context.Context, OwnerSnapshot, Fence) (CoreMutationResult, error)
	Rollback(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) (CoreMutationResult, error)
}
type CoreMutationResult struct{ Restarted bool }
type SingBoxRestartAdapter interface {
	Restart(context.Context, Fence) error
}
type ListenerOwnershipAdapter interface {
	Current(context.Context, string, string, int) (OwnerSnapshot, error)
	Manifest(context.Context) ([]OwnerSnapshot, error)
}
type FallbackTargetAdapter interface {
	Prepare(context.Context, OwnerSnapshot, Fence) error
	AbortPrepare(context.Context, OwnerSnapshot, Fence) error
	Apply(context.Context, OwnerSnapshot, Fence) error
	Rollback(context.Context, OwnerSnapshot, Fence) error
}
type HealthAdapter interface {
	Check(context.Context, []HealthTarget) ([]HealthResult, error)
}
type HelperAdapter interface {
	Capabilities(context.Context) (Capabilities, error)
}
type RecoveryUXAdapter interface {
	Record(context.Context, RecoveryFacts) error
}
type DurableSnapshotAdapter interface {
	Checkpoint(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) error
	MarkMutation(context.Context, Fence) error
	HasMutation(context.Context, Fence) (bool, error)
}
type ExactListenerAdapter interface {
	Verify(context.Context, OwnerSnapshot, Fence) error
}
type RecoveryFacts struct {
	OperationID string `json:"operationId"`
	State       string `json:"state"`
	ResourceID  string `json:"resourceId"`
	Protocol    string `json:"protocol"`
	Listen      string `json:"listen"`
	Port        int    `json:"port"`
	FromOwner   string `json:"fromOwner"`
	ToOwner     string `json:"toOwner"`
}

type JournalStore interface {
	CreatePortOperation(context.Context, protectionrepository.PortOperationModel) (protectionrepository.PortOperationModel, bool, error)
	PortOperation(context.Context, string) (protectionrepository.PortOperationModel, error)
	UpdatePortOperationFenced(context.Context, protectionrepository.FencedPortOperationUpdate) (protectionrepository.PortOperationModel, error)
	ListPortOperations(context.Context, []string) ([]protectionrepository.PortOperationModel, error)
}

type Service struct {
	Journal    JournalStore
	Operations *protectionoperations.Manager
	Inbound    InboundDraftAdapter
	Restart    SingBoxRestartAdapter
	Ownership  ListenerOwnershipAdapter
	Fallback   FallbackTargetAdapter
	Health     HealthAdapter
	Helper     HelperAdapter
	Snapshot   DurableSnapshotAdapter
	Listener   ExactListenerAdapter
	Recovery   RecoveryUXAdapter
	Now        func() time.Time

	mu        sync.Mutex
	cancelled map[string]bool
	initErr   error
}
