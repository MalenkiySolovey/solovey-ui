package helper

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type LockValidator interface {
	ValidateHelperLock(context.Context, string, string, string, int) error
}

type readLockValidator interface {
	ValidateHelperReadLock(context.Context, string, string, string, int) error
}

type listenerLockValidator interface {
	ValidateHelperListener(context.Context, string, string, int, string, string, int, int) error
}

type InvocationFacts struct {
	ExitClass       string
	StdoutTruncated bool
	StderrTruncated bool
}

type Invoker interface {
	Invoke(context.Context, Request) (Response, InvocationFacts, error)
}

type helperIdentityInvoker interface {
	HelperIdentityRevision() string
}

type ExecutionMetadata struct {
	HelperIdentityRevision        string
	CapabilityRevision            string
	ListenerOwnerContractRevision string
	ListenerOwnerObserverRevision string
}

type AuditEvent struct {
	Phase           string
	Operation       Operation
	OperationID     string
	InstanceID      string
	LockRevision    int
	OK              bool
	Code            ErrorCode
	DurationMillis  int64
	ExitClass       string
	StdoutTruncated bool
	StderrTruncated bool
}

type AuditRecorder interface {
	RecordHelperAudit(context.Context, AuditEvent) error
}

type Client struct {
	root    ManagedRoot
	locks   LockValidator
	invoker Invoker
	audit   AuditRecorder
	now     func() time.Time
}

func NewClient(root ManagedRoot, locks LockValidator, invoker Invoker, audit AuditRecorder) (*Client, error) {
	if root.path == "" {
		return nil, errors.New("managed runtime root is required")
	}
	if locks == nil {
		return nil, errors.New("operation lock validator is required")
	}
	if invoker == nil {
		return nil, errors.New("helper invoker is required")
	}
	if audit == nil {
		return nil, errors.New("redacted helper audit recorder is required")
	}
	return &Client{root: root, locks: locks, invoker: invoker, audit: audit, now: time.Now}, nil
}

func (c *Client) Execute(ctx context.Context, request Request) (Response, error) {
	response, _, err := c.ExecuteWithMetadata(ctx, request)
	return response, err
}

// ExecuteWithMetadata returns only bounded identity revisions that were
// re-attested during this exact invocation. Owner observations bind these
// revisions into their frozen host-surface snapshot.
func (c *Client) ExecuteWithMetadata(ctx context.Context, request Request) (Response, ExecutionMetadata, error) {
	started := c.now()
	metadata := ExecutionMetadata{}
	if invoker, ok := c.invoker.(helperIdentityInvoker); ok {
		metadata.HelperIdentityRevision = invoker.HelperIdentityRevision()
	}
	if err := request.Validate(c.root); err != nil {
		code, reason := CodeInvalidRequest, "request_validation_failed"
		if errors.Is(err, ErrManagedPathForbidden) {
			code, reason = CodePathForbidden, "path_forbidden"
		}
		response := responseError(request, code, reason)
		c.recordAudit(ctx, request, response, InvocationFacts{ExitClass: "not_started"}, started)
		return response, metadata, err
	}
	kind, err := request.RequiredLockKind()
	if err != nil {
		response := responseError(request, CodeInvalidRequest, err.Error())
		c.recordAudit(ctx, request, response, InvocationFacts{ExitClass: "not_started"}, started)
		return response, metadata, err
	}
	if !request.UnlockedReadOnly() {
		if err := c.validateOperationLock(ctx, request, kind); err != nil {
			response := responseError(request, CodeMissingCapability, "operation_lock_required")
			c.recordAudit(ctx, request, response, InvocationFacts{ExitClass: "not_started"}, started)
			return response, metadata, fmt.Errorf("operation lock validation failed: %w", err)
		}
		if request.Operation == OperationListenerProbe && request.ListenerProbe != nil && request.ListenerProbe.Purpose == ProbePortHandoff {
			validator, ok := c.locks.(listenerLockValidator)
			if !ok {
				response := responseError(request, CodeMissingCapability, "exact_listener_fence_required")
				c.recordAudit(ctx, request, response, InvocationFacts{ExitClass: "not_started"}, started)
				return response, metadata, errors.New("operation lock cannot validate exact listener identity")
			}
			if err := validator.ValidateHelperListener(ctx, request.Correlation.OperationID, request.Correlation.InstanceID, request.Correlation.LockRevision, request.ListenerProbe.Network, request.ListenerProbe.Address, request.ListenerProbe.Port, request.ListenerProbe.ExpectedPID); err != nil {
				response := responseError(request, CodeMissingCapability, "exact_listener_fence_required")
				c.recordAudit(ctx, request, response, InvocationFacts{ExitClass: "not_started"}, started)
				return response, metadata, fmt.Errorf("exact listener lock validation failed: %w", err)
			}
		}
	}

	capabilities, facts, err := c.negotiate(ctx, request.Correlation.InstanceID)
	if err != nil {
		code, reason := CodeMissingCapability, "helper_version_mismatch"
		if errors.Is(err, ErrHelperIdentityMismatch) {
			reason = "helper_identity_mismatch"
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code, reason = CodeTimeout, "timeout"
		} else if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			code, reason = CodeCanceled, "canceled"
		}
		response := responseError(request, code, reason)
		c.recordAudit(ctx, request, response, facts, started)
		return response, metadata, err
	}
	metadata.CapabilityRevision = capabilities.Revision
	metadata.ListenerOwnerContractRevision = capabilities.ListenerOwner.ContractRevision
	metadata.ListenerOwnerObserverRevision = capabilities.ListenerOwner.ObserverRevision
	if request.Operation != OperationCapabilities && !CapabilityAvailable(capabilities, request.Operation) {
		response := responseError(request, CodeMissingCapability, capabilityReason(capabilities, request.Operation))
		c.recordAudit(ctx, request, response, facts, started)
		return response, metadata, nil
	}
	if request.Operation == OperationCapabilities {
		response := Response{
			ProtocolVersion: ProtocolVersion, HelperVersion: HelperVersion,
			Correlation: request.Correlation, Operation: request.Operation, OK: true,
			Capabilities: capabilities,
		}
		c.recordAudit(ctx, request, response, facts, started)
		return response, metadata, nil
	}
	if !request.UnlockedReadOnly() {
		if err := c.recordAttempt(ctx, request, started); err != nil {
			response := responseError(request, CodeMissingCapability, "audit_unavailable")
			c.recordAudit(ctx, request, response, InvocationFacts{ExitClass: "not_started"}, started)
			return response, metadata, fmt.Errorf("helper audit unavailable: %w", err)
		}
	}

	timeout := timeoutFor(request.Operation)
	invokeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, facts, err := c.invoker.Invoke(invokeCtx, request)
	if err != nil {
		code := CodeProcessFailed
		reason := "helper_process_failed"
		if errors.Is(err, ErrHelperIdentityMismatch) {
			reason = "helper_identity_mismatch"
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			code, reason = CodeTimeout, "timeout"
		} else if errors.Is(err, context.Canceled) || errors.Is(invokeCtx.Err(), context.Canceled) {
			code, reason = CodeCanceled, "canceled"
		}
		response = responseError(request, code, reason)
		c.recordAudit(ctx, request, response, facts, started)
		return response, metadata, err
	}
	if response.ProtocolVersion != ProtocolVersion || !compatibleHelperVersion(response.HelperVersion) ||
		response.Correlation != request.Correlation || response.Operation != request.Operation {
		mismatch := responseError(request, CodeMissingCapability, "helper_version_mismatch")
		c.recordAudit(ctx, request, mismatch, facts, started)
		return mismatch, metadata, errors.New("helper response correlation or version mismatch")
	}
	if !request.UnlockedReadOnly() || !response.OK || response.SSHRecovery != nil && len(response.SSHRecovery.Observations) > 0 || request.Operation == OperationListenerOwnerObserve {
		c.recordAudit(ctx, request, response, facts, started)
	}
	return response, metadata, nil
}

func (c *Client) validateOperationLock(ctx context.Context, request Request, kind string) error {
	if request.Operation == OperationNginxVerify {
		if validator, ok := c.locks.(readLockValidator); ok {
			return validator.ValidateHelperReadLock(ctx, request.Correlation.OperationID, request.Correlation.InstanceID, kind, request.Correlation.LockRevision)
		}
	}
	return c.locks.ValidateHelperLock(ctx, request.Correlation.OperationID, request.Correlation.InstanceID, kind, request.Correlation.LockRevision)
}

func (c *Client) negotiate(ctx context.Context, instanceID string) (*CapabilitiesResult, InvocationFacts, error) {
	request := Request{
		ProtocolVersion: ProtocolVersion,
		Correlation:     Correlation{OperationID: "capabilities", InstanceID: instanceID},
		Operation:       OperationCapabilities,
		Capabilities:    &CapabilitiesRequest{},
	}
	invokeCtx, cancel := context.WithTimeout(ctx, timeoutFor(OperationCapabilities))
	defer cancel()
	response, facts, err := c.invoker.Invoke(invokeCtx, request)
	if err != nil {
		return nil, facts, err
	}
	if !response.OK || response.ProtocolVersion != ProtocolVersion || !compatibleHelperVersion(response.HelperVersion) ||
		response.Operation != OperationCapabilities || response.Correlation != request.Correlation {
		return nil, facts, errors.New("helper_version_mismatch")
	}
	if err := validateNegotiation(response.Capabilities); err != nil {
		return nil, facts, err
	}
	return response.Capabilities, facts, nil
}

func (c *Client) recordAudit(ctx context.Context, request Request, response Response, facts InvocationFacts, started time.Time) {
	// Deliberately omit every operation payload, path, content, stdout, stderr,
	// reason and environment value. Only bounded correlation and result facts
	// cross the audit boundary.
	_ = c.audit.RecordHelperAudit(ctx, AuditEvent{
		Phase:     "result",
		Operation: request.Operation, OperationID: request.Correlation.OperationID,
		InstanceID: request.Correlation.InstanceID, LockRevision: request.Correlation.LockRevision,
		OK: response.OK, Code: response.Code, DurationMillis: c.now().Sub(started).Milliseconds(),
		ExitClass: facts.ExitClass, StdoutTruncated: facts.StdoutTruncated, StderrTruncated: facts.StderrTruncated,
	})
}

func (c *Client) recordAttempt(ctx context.Context, request Request, started time.Time) error {
	return c.audit.RecordHelperAudit(ctx, AuditEvent{
		Phase:     "attempt",
		Operation: request.Operation, OperationID: request.Correlation.OperationID,
		InstanceID: request.Correlation.InstanceID, LockRevision: request.Correlation.LockRevision,
		DurationMillis: c.now().Sub(started).Milliseconds(), ExitClass: "not_started",
	})
}

func responseError(request Request, code ErrorCode, reason string) Response {
	return Response{
		ProtocolVersion: ProtocolVersion, HelperVersion: HelperVersion,
		Correlation: request.Correlation, Operation: request.Operation,
		Code: code, Reason: reason,
	}
}

func timeoutFor(operation Operation) time.Duration {
	switch operation {
	case OperationNFTApply, OperationNginxInstall, OperationNginxSwitch, OperationNginxReload:
		return 60 * time.Second
	case OperationListenerOwnerObserve:
		// The exact active executable SHA-256 is intentionally recomputed from
		// /proc/<MainPID>/exe. Large full-profile binaries on slow guest storage
		// can exceed the ordinary read-only deadline while still remaining well
		// within this explicit bounded observation window.
		return 60 * time.Second
	case OperationNFTRollback, OperationNginxRestore:
		return 120 * time.Second
	default:
		return 15 * time.Second
	}
}
