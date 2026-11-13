package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestFallbackFeatureCompatibilityPathsOnlyAliasTheirSourceOwner(t *testing.T) {
	for relative, canonical := range map[string]string{
		"components/fallback-html/domain/feature.go":    "domain",
		"components/fallback-html/service/feature.go":   "service",
		"components/fallback-html/lifecycle/durable.go": "lifecycle",
	} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), filepath.FromSlash(relative)), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(file.Imports) != 1 {
			t.Fatalf("%s gained a private owner dependency", relative)
		}
		imported, err := strconv.Unquote(file.Imports[0].Path.Value)
		if err != nil || imported != "github.com/MalenkiySolovey/solovey-component-fallback-html/"+canonical {
			t.Fatalf("%s lost canonical feature source", relative)
		}
		for _, declaration := range file.Decls {
			block, ok := declaration.(*ast.GenDecl)
			if !ok {
				t.Fatalf("%s gained editable feature behavior", relative)
			}
			for _, spec := range block.Specs {
				var values []ast.Expr
				switch value := spec.(type) {
				case *ast.ImportSpec:
					continue
				case *ast.TypeSpec:
					if !value.Assign.IsValid() {
						t.Fatalf("%s gained another type owner", relative)
					}
					values = []ast.Expr{value.Type}
				case *ast.ValueSpec:
					values = value.Values
				default:
					t.Fatalf("%s gained a non-alias declaration", relative)
				}
				for _, value := range values {
					selector, ok := value.(*ast.SelectorExpr)
					if !ok {
						t.Fatalf("%s transformed feature behavior", relative)
					}
					owner, ok := selector.X.(*ast.Ident)
					if !ok || owner.Name != "feature" {
						t.Fatalf("%s bypassed canonical feature source", relative)
					}
				}
			}
		}
	}
}

func TestFallbackFeatureHasOneEditableSource(t *testing.T) {
	for _, relative := range []string{
		"frontend/index.ts", "frontend/usePublicSite.ts", "frontend/publicSiteLogic.ts", "frontend/views/PublicSite.vue", "frontend/locales/en.ts", "frontend/locales/ru.ts",
		"domain/models.go", "domain/path.go", "service/assets.go", "service/import.go",
		"service/publish.go", "service/remote_templates.go", "service/reservation_guard.go",
		"service/runtime.go", "service/service.go", "service/targets.go", "service/health.go",
		"service/http_host.go", "service/host.go", "service/target_observation.go",
		"service/decoy_script.go", "service/decoy-interactivity.js", "service/node_artifact.go",
	} {
		filename := filepath.Join(moduleRoot(t), "components", "fallback-html", filepath.FromSlash(relative))
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Errorf("retired editable feature source returned: %s (%v)", relative, err)
		}
	}
}
