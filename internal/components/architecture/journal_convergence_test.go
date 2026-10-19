package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestCommonJournalCannotAcquireOwnerStorageOrUnboundedPayloads(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "internal", "ops", "operationcoordination", "journal.go")
	allowed := map[string]bool{"context": true, "errors": true, "strconv": true, "strings": true}
	for _, imported := range fileImports(t, path) {
		if !allowed[imported] {
			t.Errorf("journal imports storage/owner execution dependency %s", imported)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "JournalEvent" {
			return true
		}
		ast.Inspect(spec.Type, func(field ast.Node) bool {
			switch field.(type) {
			case *ast.InterfaceType, *ast.MapType, *ast.ArrayType:
				t.Error("common journal gained an arbitrary payload")
			}
			return true
		})
		return false
	})
}
