package durable

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Storage-independent contracts and feature hooks must stay buildable without
// Product composition, concrete storage, HTTP frameworks or generic execution.
func TestDurableContractsAndFeatureHooksHaveNoBackendDependencies(t *testing.T) {
	for _, dir := range []string{".", "../../components/fallback-html/lifecycle"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("inspect source: %v", err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				name, _ := strconv.Unquote(spec.Path.Value)
				if name == "context" || name == "errors" || name == "time" || name == "github.com/MalenkiySolovey/solovey-host/hostref" || name == "github.com/MalenkiySolovey/solovey-ui/componenthost/durable" {
					continue
				}
				t.Errorf("Backend or unreviewed dependency in %s: %s", file, name)
			}
		}
	}
}
