package privilegedbroker

import (
	"errors"
	"strings"
	"testing"
)

func TestStartupDiagnosticContainsOnlySafeCapabilityContext(t *testing.T) {
	cause := errors.New("secret=/tmp/private command-output=do-not-log")
	err := StartupFailure("ssh", "service.supervision", "procd", "required_executable_unavailable", cause)
	if got, want := err.Error(), "owner=ssh capability=service.supervision adapter=procd reason=required_executable_unavailable"; got != want {
		t.Fatalf("startup diagnostic = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("startup diagnostic discarded its internal cause")
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "command-output") {
		t.Fatalf("startup diagnostic exposed cause text: %q", err)
	}
}

func TestStartupDiagnosticRejectsUnboundedFacts(t *testing.T) {
	err := StartupFailure("ssh\nsecret", "service.supervision", "procd", "required_executable_unavailable", errors.New("cause"))
	if got, want := err.Error(), "owner=broker capability=broker.startup adapter=unknown reason=startup_failed"; got != want {
		t.Fatalf("invalid startup diagnostic = %q, want %q", got, want)
	}
}

func TestEnsureStartupFailurePreservesOwnerLocalContext(t *testing.T) {
	original := StartupFailure("ssh", "ssh.security_log_evidence", "logread", "required_executable_unavailable", errors.New("cause"))
	if got := EnsureStartupFailure(original, "broker", "broker.handlers", "ssh", "registration_failed"); got != original {
		t.Fatal("composition root replaced owner-local startup context")
	}
}
