package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestCommonHealthCannotImportRepairOrSemanticOwnerExecutors(t *testing.T) {
	allowed := map[string]bool{"context": true, "errors": true, "strings": true, "sync": true,
		"time": true, "crypto/sha256": true, "encoding/hex": true, modulePath + "/componenthost/hostref": true}
	for _, relative := range []string{"componenthost/health/registry.go", "componenthost/health/evidence.go"} {
		for _, imported := range fileImports(t, filepath.Join(moduleRoot(t), filepath.FromSlash(relative))) {
			if !allowed[imported] {
				t.Errorf("common Health imports repair/probe/owner dependency %s", imported)
			}
		}
	}
}

func TestMigratedHealthConsumersRequireProofRatherThanLegacyStatus(t *testing.T) {
	for _, target := range []struct{ path, function string }{{"app/update_health.go", "updateHealth"}, {"service/doctor.go", "Run"}} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), filepath.FromSlash(target.path)), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		fn := findFunction(file, target.function)
		found, bypass := false, false
		if fn == nil {
			t.Fatalf("missing proof consumer %s", target.path)
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "componenthealth" {
				return true
			}
			found = found || selector.Sel.Name == "Require"
			bypass = bypass || selector.Sel.Name == "Check"
			return true
		})
		if !found || bypass {
			t.Errorf("%s bypasses fresh revision-bound Health proof", target.path)
		}
	}
}
