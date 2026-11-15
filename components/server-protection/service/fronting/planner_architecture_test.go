package fronting

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
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

func TestFrontingUsesTypedOwnersForPlatformAndExecutableInspection(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fronting source location unavailable")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(current), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			value := strings.Trim(imported.Path.Value, "\"")
			if value == "os" || value == "os/exec" || value == "syscall" || value == "unsafe" || strings.HasPrefix(value, "golang.org/x/sys/") {
				t.Errorf("feature inspection gained platform authority through %s in %s", value, filepath.Base(name))
			}
		}
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
