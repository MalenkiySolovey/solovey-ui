package fronting

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadOnlyPlannerHasNoMutationPersistenceOrPublicTransportDependency(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("planner source location unavailable")
	}
	root := filepath.Dir(current)
	files := []string{"runtime_identity_v2.go", "planner_contracts_v2.go", "planner_v2.go"}
	forbiddenImports := map[string]bool{"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper": true, "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations": true, "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts": true, "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository": true, "net/http": true}
	for _, name := range files {
		path := filepath.Join(root, name)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			value := imported.Path.Value[1 : len(imported.Path.Value)-1]
			if forbiddenImports[value] {
				t.Errorf("%s imports forbidden read-only dependency %q", name, value)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "WriteFile" {
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "os" {
					t.Errorf("%s writes through os.WriteFile", name)
				}
			}
			return true
		})
	}
}

func TestReadOnlyPlannerDefinesNoPackageMutableState(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("planner source location unavailable")
	}
	root := filepath.Dir(current)
	for _, name := range []string{"runtime_identity_v2.go", "planner_contracts_v2.go", "planner_v2.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if ok && general.Tok == token.VAR {
				t.Errorf("%s defines package-level mutable state", name)
			}
		}
	}
}

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
