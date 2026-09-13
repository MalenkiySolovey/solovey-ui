package helper

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestSSHRecoveryAdapterHasOnlyTheTypedSSHOwnerBoundary(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "ssh_recovery_backend.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowedImports := map[string]bool{
		"context": true,
		"errors":  true,
		"github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker": true,
	}
	for _, imported := range parsed.Imports {
		path, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil || !allowedImports[path] {
			t.Fatalf("SSH recovery adapter imports authority outside its typed owner boundary: %s", imported.Path.Value)
		}
	}
	foundOwnerField, foundOwnerConstructor := false, false
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.Field:
			if selector, ok := value.Type.(*ast.SelectorExpr); ok {
				if identifier, qualified := selector.X.(*ast.Ident); qualified && identifier.Name == "sshbroker" && selector.Sel.Name == "RecoveryObserver" {
					foundOwnerField = true
				}
			}
		case *ast.CallExpr:
			if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
				if identifier, qualified := selector.X.(*ast.Ident); qualified && identifier.Name == "sshbroker" && selector.Sel.Name == "NewRecoveryObserver" {
					foundOwnerConstructor = true
				}
			}
		}
		return true
	})
	if !foundOwnerField || !foundOwnerConstructor {
		t.Fatalf("SSH recovery adapter is not composed exclusively from sshbroker.RecoveryObserver: field=%v constructor=%v", foundOwnerField, foundOwnerConstructor)
	}
}

func TestBrokerRegistryContainsOnlyTypedSemanticOperations(t *testing.T) {
	registry := broker.NewRegistry()
	if err := RegisterBrokerHandlers(registry, testManagedRoot(t)); err != nil {
		t.Fatal(err)
	}
	verbs := registry.Verbs(broker.RolePanel)
	if len(verbs) != 14 {
		t.Fatalf("registered server-protection verbs = %d, want 14: %v", len(verbs), verbs)
	}
	for _, verb := range verbs {
		if strings.Contains(string(verb), "artifact.manage") {
			t.Fatalf("generic privileged mutation verb was registered: %q", verb)
		}
	}
	if _, _, ok := brokerVerb(Operation("artifact.manage")); ok {
		t.Fatal("removed generic artifact operation still maps to a broker verb")
	}
	for _, capability := range DefaultCapabilities().Capabilities {
		if string(capability.Operation) == "artifact.manage" {
			t.Fatalf("generic artifact operation remains advertised: %#v", capability)
		}
	}
}
