package architecture

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPortableSubscriptionSourceHasNoBackendAuthority(t *testing.T) {
	root := moduleRoot(t)
	source := selectedSubscriptionSource(t, root)
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
			allowedOwner := strings.HasPrefix(name, "github.com/MalenkiySolovey/solovey-host/") || strings.HasPrefix(name, subscriptionSourceModule+"/")
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

const subscriptionSourceModule = "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions"

func selectedSubscriptionSource(t *testing.T, root string) string {
	t.Helper()
	command := exec.Command("go", "list", "-m", "-json", subscriptionSourceModule)
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		t.Fatalf("selected subscription source unavailable: %v", err)
	}
	var metadata struct {
		Path, Dir, Version string
		Replace            json.RawMessage
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Path != subscriptionSourceModule || !filepath.IsAbs(metadata.Dir) || metadata.Version == "" || len(metadata.Replace) != 0 {
		t.Fatalf("subscription source is not an immutable selected module: %s", data)
	}
	return metadata.Dir
}

func TestSubscriptionFeatureHasOneEditableSource(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{"source", "frontend"} {
		directory := filepath.Join(root, "components/remote-outbound-subscriptions", relative)
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		t.Errorf("retired editable subscription source returned: %s", directory)
	}
}
