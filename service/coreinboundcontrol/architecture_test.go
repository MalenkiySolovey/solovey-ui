package coreinboundcontrol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPackageImportsStayInsideNarrowCoreBoundary(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller location unavailable")
	}
	directory := filepath.Dir(currentFile)
	matches, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	parsedFiles := make([]*ast.File, 0, len(matches))
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		parsedFiles = append(parsedFiles, parsed)
	}
	allowedModuleImports := map[string]bool{
		"github.com/MalenkiySolovey/solovey-ui/componenthost/health":        true,
		"github.com/MalenkiySolovey/solovey-ui/componenthost/resources":     true,
		"github.com/MalenkiySolovey/solovey-ui/core/registry":               true,
		"github.com/MalenkiySolovey/solovey-ui/database/model":              true,
		"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation": true,
	}
	for _, file := range parsedFiles {
		for _, importSpec := range file.Imports {
			path, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(path, "github.com/MalenkiySolovey/solovey-ui/") && !allowedModuleImports[path] {
				t.Fatalf("unexpected product import %q", path)
			}
		}
	}
}
