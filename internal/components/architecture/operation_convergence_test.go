package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestMigratedOwnerTransitionsUseAtomicCoordination(t *testing.T) {
	for _, target := range []struct{ path, function string }{
		{"service/update/lifecycle.go", "update"},
		{"service/datalifecycle/drop.go", "advance"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), filepath.FromSlash(target.path)), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		fn := findFunction(file, target.function)
		if fn == nil || !coordinatedTransition(fn.Body) {
			t.Errorf("%s.%s bypasses shared CAS/journal coordination", target.path, target.function)
		}
	}
}

func coordinatedTransition(body *ast.BlockStmt) bool {
	shared, private := false, false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "operationcoordination" && sel.Sel.Name == "CommitTransition" {
				shared = true
			}
			if sel.Sel.Name == "Transaction" || sel.Sel.Name == "Updates" || sel.Sel.Name == "Create" {
				private = true
			}
		}
		return true
	})
	return shared && !private
}

func TestTransitionRatchetDistinguishesOwnerPolicyFromDuplicateMechanics(t *testing.T) {
	for _, fixture := range []struct {
		source  string
		allowed bool
	}{
		{`package fixture; func update() { validateOwner(); operationcoordination.CommitTransition() }`, true},
		{`package fixture; func update() { operationcoordination.CommitTransition(); db.Transaction() }`, false},
		{`package fixture; func update() { db.Updates(); db.Create() }`, false},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", fixture.source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := coordinatedTransition(findFunction(file, "update").Body); got != fixture.allowed {
			t.Fatalf("ratchet allowed=%v want=%v", got, fixture.allowed)
		}
	}
}

func TestCommonPreflightRemainsAReadOnlySemanticFactEvaluator(t *testing.T) {
	allowed := map[string]bool{"context": true, "strings": true, "time": true, modulePath + "/internal/deployment": true}
	root := filepath.Join(moduleRoot(t), "internal", "ops", "preflight")
	walkGoFiles(t, root, func(path string) {
		if filepath.Ext(path) != ".go" || len(path) >= 8 && path[len(path)-8:] == "_test.go" {
			return
		}
		for _, imported := range fileImports(t, path) {
			if !allowed[imported] {
				t.Errorf("common preflight imports executable/owner dependency %s", imported)
			}
		}
	})
}
