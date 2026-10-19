package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestSecretReferenceHasNoBackendRawProjectionOrRevealOperation(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "componenthost", "secretref", "reference.go")
	allowed := map[string]bool{"errors": true, "strings": true, modulePath + "/internal/ops/operationcoordination": true}
	for _, imported := range fileImports(t, path) {
		if !allowed[imported] {
			t.Errorf("SecretRef acquired storage/crypto/transport owner: %s", imported)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		ast.Inspect(spec.Type, func(field ast.Node) bool {
			switch field.(type) {
			case *ast.InterfaceType, *ast.MapType, *ast.ArrayType:
				t.Error("SecretRef metadata gained arbitrary/raw payload")
			}
			return true
		})
		return false
	})
}

func TestMigratedTelegramCredentialUsesScopedSecretAccess(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "components", "telegram", "internal", "settings", "settings.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := findFunction(file, "GetTelegramBotToken")
	if fn == nil {
		t.Fatal("missing credential owner")
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "AccessComponentSecret" {
			found = true
		}
		switch selector.Sel.Name {
		case "GetComponentSettingString", "GetComponentSettingSecretBytes":
			t.Error("migrated token bypasses SecretRef owner access")
		}
		return true
	})
	if !found {
		t.Error("Telegram token no longer consumes SecretRef")
	}
}
