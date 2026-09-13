package sshbroker

import (
	"context"
	"errors"
)

type brokerDiagnosticError interface {
	BrokerDiagnostic() (owner, reason, stage, errno, proofMethod string)
}

// listenerAuthorityError carries only a closed owner-local class across the
// privileged boundary. Public broker errors remain sanitized.
type listenerAuthorityError struct {
	reason      string
	stage       string
	errno       string
	proofMethod string
	cause       error
}

func (e *listenerAuthorityError) Error() string {
	if e == nil {
		return ""
	}
	return "SSH listener authority: " + e.reason
}

func (e *listenerAuthorityError) BrokerDiagnostic() (owner, reason, stage, errno, proofMethod string) {
	if e == nil {
		return "", "", "", "", ""
	}
	return "ssh_listener_authority", e.reason, e.stage, e.errno, e.proofMethod
}

// Unwrap keeps ordinary Go error-chain behavior for internal callers without
// exposing the cause through Error or the public broker response.
func (e *listenerAuthorityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func listenerAuthorityFailure(reason, stage, errno, proofMethod string) error {
	return &listenerAuthorityError{reason: reason, stage: stage, errno: errno, proofMethod: proofMethod}
}

// listenerAuthorityWrap classifies an untyped owner-local failure at the
// boundary that knows its meaning. An already classified diagnostic is
// returned unchanged so its exact owner, stage, reason, errno and counters
// survive any number of normal %w wrappers.
func listenerAuthorityWrap(err error, reason, stage, errno, proofMethod string) error {
	if err == nil {
		return nil
	}
	var classified brokerDiagnosticError
	if errors.As(err, &classified) {
		return err
	}
	return &listenerAuthorityError{reason: reason, stage: stage, errno: errno, proofMethod: proofMethod, cause: err}
}

func ensureSSHObserveDiagnostic(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return listenerAuthorityWrap(err, "observation_failed", "authority_validate", "", "")
}
