package serverprotection

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestServicePackagesStayTransportIndependent(t *testing.T) {
	err := filepath.WalkDir("service", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			if imported.Path.Value == `"github.com/gin-gonic/gin"` {
				t.Errorf("service package imports HTTP transport: %s", filepath.ToSlash(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUDPHTTPTransportDoesNotOwnMutationAuthority(t *testing.T) {
	path := filepath.Join("api", "udp_guard.go")
	parsed, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbiddenCalls := map[string]bool{"Prepare": true, "Apply": true, "Rollback": true, "UDPGuardStates": true, "SaveUDPGuardState": true, "FirewallAuthority": true}
	forbiddenIdentifiers := map[string]bool{"DefaultProtocolProbesV1": true, "PostApplyHealth": true}
	ast.Inspect(parsed, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && forbiddenCalls[selector.Sel.Name] {
				if !isUDPServiceSelector(selector.X) {
					t.Errorf("UDP HTTP transport owns service authority through %s", selector.Sel.Name)
				}
			}
			if identifier, ok := call.Fun.(*ast.Ident); ok && forbiddenCalls[identifier.Name] {
				t.Errorf("UDP HTTP transport owns service authority through %s", identifier.Name)
			}
		}
		if identifier, ok := node.(*ast.Ident); ok && forbiddenIdentifiers[identifier.Name] {
			t.Errorf("UDP HTTP transport owns service authority through %s", identifier.Name)
		}
		return true
	})
}

func isUDPServiceSelector(node ast.Expr) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "udpService"
}
