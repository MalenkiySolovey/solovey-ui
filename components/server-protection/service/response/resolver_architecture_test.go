package response

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSNIPrereadDowngradeHasNoLeaseTopologyOrAppliedActionSurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "resolver.go")
	parsed, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	decisionValue := ""
	hasForcedDecoy := false
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != gotoken.CONST {
			continue
		}
		for _, specification := range general.Specs {
			valueSpec, ok := specification.(*ast.ValueSpec)
			if !ok || len(valueSpec.Names) == 0 || valueSpec.Names[0].Name != "SNIPrereadActionMapDecisionV2" || len(valueSpec.Values) != 1 {
				continue
			}
			if literal, ok := valueSpec.Values[0].(*ast.BasicLit); ok && literal.Kind == gotoken.STRING && len(literal.Value) >= 2 {
				decisionValue = literal.Value[1 : len(literal.Value)-1]
			}
		}
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if identifier.Name == "ForcedSameSubjectDecoyRoute" {
			hasForcedDecoy = true
		}
		if map[string]bool{"EndpointLeaseProvider": true, "ProviderTargetReservation": true, "PrepareV2": true, "ApplyV2": true, "AppliedActionV1": true}[identifier.Name] {
			t.Fatalf("downgrade-only resolver gained mutation/action evidence surface %q", identifier.Name)
		}
		return true
	})
	if decisionValue != "DOWNGRADE_ONLY" || !hasForcedDecoy {
		t.Fatal("SNI downgrade capability is not explicit")
	}
}
