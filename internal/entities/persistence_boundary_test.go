package entities

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestR4DomainPackagesDoNotImportConcreteDatabaseLifecycle(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	for _, relativeRoot := range []string{"internal/entities", "internal/settings", "internal/subscriptions"} {
		root := filepath.Join(repositoryRoot, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range parsed.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				for _, forbidden := range []string{
					"github.com/MalenkiySolovey/solovey-ui/database/sqlite",
					"github.com/MalenkiySolovey/solovey-ui/database/migration",
					"github.com/MalenkiySolovey/solovey-ui/database/backup",
					"github.com/MalenkiySolovey/solovey-ui/database/restorestate",
				} {
					if name == forbidden || strings.HasPrefix(name, forbidden+"/") {
						t.Errorf("%s imports concrete database lifecycle package %s", filepath.ToSlash(path), name)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
