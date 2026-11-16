package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPortableSubscriptionSourceHasNoBackendAuthority(t *testing.T) {
	root := moduleRoot(t)
	source := filepath.Join(root, "components/remote-outbound-subscriptions/source")
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{}
		for _, spec := range file.Imports {
			name, _ := strconv.Unquote(spec.Path.Value)
			alias := filepath.Base(name)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			imports[alias] = name
			allowedOwner := strings.HasPrefix(name, "github.com/MalenkiySolovey/solovey-host/") || strings.HasPrefix(name, modulePath+"/components/remote-outbound-subscriptions/source/")
			forbiddenStandard := name == "os" || name == "syscall" || name == "net" || name == "database/sql" || strings.HasPrefix(name, "os/") || strings.HasPrefix(name, "net/") && name != "net/http" && name != "net/url"
			if !allowedOwner && (strings.Contains(strings.Split(name, "/")[0], ".") || forbiddenStandard) {
				t.Errorf("portable source imports Backend/execution dependency %s: %s", name, path)
			}
		}
		for _, decl := range file.Decls {
			if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "init" {
				t.Errorf("source import starts activity: %s", path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if qualifier, ok := selector.X.(*ast.Ident); ok && imports[qualifier.Name] == "net/http" {
				t.Errorf("feature executes HTTP rather than consuming Backend transport: %s", path)
			}
			if selector.Sel.Name == "Do" {
				t.Errorf("feature bypasses Backend HTTP transport: %s", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
