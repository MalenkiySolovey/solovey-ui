package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSharedHostCompatibilityPathsOnlyForwardToCanonicalSource(t *testing.T) {
	for relative, canonical := range map[string]string{
		"componenthost/hostref/local.go":                    "hostref",
		"componenthost/secretref/reference.go":              "secretref",
		"componenthost/ports.go":                            "componenthost",
		"internal/components/manifest/host_api.go":          "componenthost/manifest",
		"internal/components/manifest/manifest.go":          "componenthost/manifest",
		"internal/components/manifest/durable.go":           "componenthost/manifest",
		"internal/components/manifest/json.go":              "componenthost/manifest",
		"internal/httpheader/sanitize.go":                   "httpheader",
		"util/redact/redact.go":                             "redact",
		"componentkit/telegram/botapi.go":                   "componentkit/telegram",
		"componentkit/telegram/transport.go":                "componentkit/telegram",
		"componenthost/resources/fronting_contracts.go":     "componenthost/resources",
		"componenthost/resources/local_proxy_contracts.go":  "componenthost/resources",
		"componenthost/resources/inbound_transport_v2.go":   "componenthost/resources",
		"componenthost/resources/interception_contracts.go": "componenthost/resources",
	} {
		t.Run(relative, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), filepath.FromSlash(relative)), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, imported := range file.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if name == "github.com/MalenkiySolovey/solovey-host/"+canonical && imported.Name != nil && imported.Name.Name == "canonical" {
					found = true
				} else if name != "time" && name != "encoding/json" && name != "net/http" && !(canonical == "componenthost/resources" && name == "context") && !(relative == "componenthost/resources/local_proxy_contracts.go" && name == "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface") {
					t.Errorf("compatibility path imports another implementation: %s", name)
				}
			}
			if !found {
				t.Fatal("canonical source import missing")
			}
			for _, declaration := range file.Decls {
				switch declaration := declaration.(type) {
				case *ast.FuncDecl:
					if declaration.Recv != nil || declaration.Name.Name == "init" || declaration.Body == nil || len(declaration.Body.List) != 1 {
						t.Fatal("compatibility path acquired an editable implementation")
					}
					returned, ok := declaration.Body.List[0].(*ast.ReturnStmt)
					if !ok || len(returned.Results) != 1 {
						t.Fatal("compatibility function must only return its canonical call")
					}
					call, ok := returned.Results[0].(*ast.CallExpr)
					canonicalName := declaration.Name.Name
					if canonical == "componenthost/resources" {
						switch canonicalName {
						case "frontingToken":
							canonicalName = "FrontingContractTokenValidV1"
						case "boundedToken":
							canonicalName = "TransportContractTokenValidV2"
						case "interceptionToken":
							canonicalName = "InterceptionContractTokenValidV1"
						}
					}
					if !ok || !canonicalSelector(call.Fun, canonicalName) {
						t.Fatal("compatibility function replaced its canonical behavior")
					}
					for _, argument := range call.Args {
						if _, ok := argument.(*ast.Ident); !ok {
							t.Fatal("compatibility function transformed an input")
						}
					}
				case *ast.GenDecl:
					for _, spec := range declaration.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							if !spec.Assign.IsValid() || !canonicalSelector(spec.Type, "") {
								t.Fatal("compatibility type became another owner")
							}
						case *ast.ValueSpec:
							for _, value := range spec.Values {
								if !canonicalSelector(value, "") {
									t.Fatal("compatibility value acquired initialization behavior")
								}
							}
						}
					}
				}
			}
		})
	}
}

func canonicalSelector(expression ast.Expr, name string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || name != "" && selector.Sel.Name != name {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == "canonical"
}
