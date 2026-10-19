package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestArtifactIdentityCannotOwnTransportOrAnotherTrustStore(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "internal", "release", "identity.go")
	allowed := map[string]bool{"crypto/sha256": true, "encoding/hex": true, "errors": true}
	for _, imported := range fileImports(t, path) {
		if !allowed[imported] {
			t.Errorf("artifact identity acquired transport/owner dependency: %s", imported)
		}
	}
}

func TestMigratedArtifactConsumersUseVerifiedIdentity(t *testing.T) {
	for _, target := range []struct{ file, function, required string }{
		{"service/update/lifecycle.go", "fetchAndVerify", "PanelArtifacts"},
		{"service/update/lifecycle.go", "validateManifestCompatibility", "ComponentArtifacts"},
		{"service/update/broker_provider.go", "downloadAndStageArtifact", "ArtifactIdentity"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), filepath.FromSlash(target.file)), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		fn := findFunction(file, target.function)
		if fn == nil {
			t.Fatalf("missing artifact consumer: %s", target.function)
		}
		found := false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == target.required {
				found = true
			}
			if ok && selector.Sel.Name == "ArtifactsFor" {
				t.Errorf("%s bypassed verified identity", target.function)
			}
			return true
		})
		if !found {
			t.Errorf("%s bypasses common signed artifact contract", target.function)
		}
	}
}
