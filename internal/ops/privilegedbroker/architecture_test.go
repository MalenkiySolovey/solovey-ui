package privilegedbroker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This guard is semantic protocol vocabulary, not implementation placement:
// the privileged API may expose only registered owner operations and never a
// generic command/shell/artifact mutation escape hatch.
func TestPrivilegedProtocolHasNoGenericCommandVerbs(t *testing.T) {
	forbiddenIdentifiers := map[string]struct{}{
		"VerbExec": {}, "VerbShell": {}, "RunUCI": {}, "RunSystemctl": {}, "RunUBus": {},
		"OperationArtifact": {}, "ArtifactRequest": {}, "ArtifactResult": {}, "ArtifactScope": {}, "ArtifactAction": {},
	}
	roots := []string{".", filepath.Join("..", "..", "..", "components", "server-protection", "service", "helper")}
	files := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(files, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.Ident:
					if _, forbidden := forbiddenIdentifiers[value.Name]; forbidden {
						t.Errorf("generic privileged identifier %q exists in %s", value.Name, path)
					}
				case *ast.BasicLit:
					if value.Kind == token.STRING {
						literal, err := strconv.Unquote(value.Value)
						if err == nil && literal == "artifact.manage" {
							t.Errorf("generic privileged verb %q exists in %s", literal, path)
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
