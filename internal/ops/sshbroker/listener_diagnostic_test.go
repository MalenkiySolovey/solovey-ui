package sshbroker

import (
	"errors"
	"fmt"
	"testing"
)

func TestListenerAuthorityValidationDefinesItsOwnBoundedReason(t *testing.T) {
	for _, test := range []struct {
		reason string
		stage  string
	}{
		{reason: "socket_not_accepting", stage: "configured_listener_match"},
		{reason: "socket_identity_mismatch", stage: "configured_endpoint_match"},
		{reason: "socket_identity_mismatch", stage: "procd_command_match"},
	} {
		t.Run(test.stage, func(t *testing.T) {
			err := listenerAuthorityFailure(test.reason, test.stage, "", "procfs_fd_inode_socket_table/v1")
			typed, ok := err.(*listenerAuthorityError)
			if !ok {
				t.Fatalf("listener authority error type = %T", err)
			}
			owner, reason, stage, errno, proof := typed.BrokerDiagnostic()
			if owner != "ssh_listener_authority" || reason != test.reason || stage != test.stage ||
				errno != "" || proof != "procfs_fd_inode_socket_table/v1" {
				t.Fatalf("owner-local validation diagnostic = %q %q %q %q %q", owner, reason, stage, errno, proof)
			}
		})
	}
}

func TestListenerAuthorityDiagnosticPreservesCauseAndExistingTypedChain(t *testing.T) {
	cause := errors.New("private host cause")
	classified := listenerAuthorityWrap(cause, "process_identity_unavailable", "process_fence", "", "")
	if !errors.Is(classified, cause) {
		t.Fatal("owner classification dropped normal Go error-chain behavior")
	}
	wrapper := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", classified))
	if got := ensureSSHObserveDiagnostic(wrapper); got != wrapper {
		t.Fatal("an existing typed diagnostic was replaced instead of preserved")
	}
	var typed brokerDiagnosticError
	if !errors.As(wrapper, &typed) {
		t.Fatal("typed diagnostic did not survive ordinary wrapping")
	}
	owner, reason, stage, errno, proof := typed.BrokerDiagnostic()
	if owner != "ssh_listener_authority" || reason != "process_identity_unavailable" || stage != "process_fence" || errno != "" || proof != "" {
		t.Fatalf("wrapped diagnostic = %q %q %q %q %q", owner, reason, stage, errno, proof)
	}
}

func TestSSHObserveFallbackClassifiesPreviouslyUntypedOwnerFailure(t *testing.T) {
	cause := errors.New("private natural owner failure")
	err := ensureSSHObserveDiagnostic(cause)
	if !errors.Is(err, cause) {
		t.Fatal("fallback classification dropped its internal cause")
	}
	var typed brokerDiagnosticError
	if !errors.As(err, &typed) {
		t.Fatal("untyped owner failure left the SSH observe boundary unclassified")
	}
	owner, reason, stage, _, _ := typed.BrokerDiagnostic()
	if owner != "ssh_listener_authority" || reason != "observation_failed" || stage != "authority_validate" {
		t.Fatalf("fallback diagnostic = %q %q %q", owner, reason, stage)
	}
}
