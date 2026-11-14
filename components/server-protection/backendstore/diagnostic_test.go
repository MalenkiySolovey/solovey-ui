package backendstore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	sqlite3 "github.com/mattn/go-sqlite3"
)

func TestBackendDriverDiagnosticKeepsCauseWithoutLoggingPrivateQueryMaterial(t *testing.T) {
	cause := fmt.Errorf("private query credential-from-query: %w", sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintCheck})
	projected := backendError(cause)
	var diagnostic durable.PersistenceDiagnosticProvider
	if !errors.As(projected, &diagnostic) || !errors.Is(projected, cause) {
		t.Fatalf("Backend error lost its private cause or public diagnostic: %v", projected)
	}
	value := diagnostic.PersistenceDiagnostic()
	if value.Primary != int(sqlite3.ErrConstraint) || value.Extended != int(sqlite3.ErrConstraintCheck) {
		t.Fatalf("driver code attribution changed: %#v", value)
	}
	message := (&operations.RuntimeRecoveryError{Step: "runtime_restore", Cause: projected}).Error()
	if strings.Contains(message, "credential-from-query") || strings.Contains(message, "private query") ||
		!strings.Contains(message, "step=runtime_restore role=runtime_recovery") || !strings.Contains(message, "sqlite_primary=19 sqlite_extended=275") {
		t.Fatalf("recovery diagnostic leaked private material or lost attribution: %s", message)
	}
	if backendError(projected) != projected {
		t.Fatal("existing diagnostic was wrapped a second time")
	}
}
