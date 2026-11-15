package fronting

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEndpointLeaseContractDefinesNoAuthorityMutationMethods(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	path := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", "..", "componenthost", "resources", "fronting_contracts.go"))
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Recv == nil || len(declaration.Recv.List) == 0 || declaration.Name == nil {
			return true
		}
		if (declaration.Name.Name == "Acquire" || declaration.Name.Name == "Renew" || declaration.Name.Name == "Activate" || declaration.Name.Name == "Release") && receiverTypeName(declaration.Recv.List[0].Type) == "EndpointLeaseV1" {
			t.Errorf("neutral lease contract gained provider mutation method %q", declaration.Name.Name)
		}
		return true
	})
}

func receiverTypeName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverTypeName(value.X)
	default:
		return ""
	}
}

func TestProductFrontingAPIHasNoDirectUnqualifiedApplySurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	directory := filepath.Dir(file)
	apiPath := filepath.Join(directory, "..", "..", "api", "fronting.go")
	api := parseArchitectureFile(t, apiPath)
	assertNoForbiddenIdentifiers(t, api, apiPath, map[string]bool{"PrepareV2": true, "ApplyV2": true})
	assertAnyIdentifiers(t, []string{apiPath}, []string{"frontingSemanticService", "FrontingStrategyPlanV2", "frontingApplyConfigured"})
}
