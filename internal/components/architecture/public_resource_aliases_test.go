package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestProductNeutralContractsHaveOneEditablePublicDefinition(t *testing.T) {
	for _, entry := range []struct {
		file, owner      string
		selected         map[string]bool
		forwardFunctions bool
	}{
		{"componenthost/hostsurface/types.go", "componenthost/hostsurface", nil, true},
		{"componenthost/hostsurface/listener_owner.go", "componenthost/hostsurface", nil, true},
		{"componenthost/resources/types.go", "componenthost/resources", map[string]bool{"ProtectableResource": true, "ProtectableResourceCapabilities": true, "ResourceSnapshot": true, "ResourceWarning": true, "ResourceError": true}, false},
		{"componenthost/resources/identity.go", "componenthost/resources", nil, true},
		{"componenthost/resources/endpoints.go", "componenthost/resources", nil, true},
		{"componenthost/resources/fingerprint.go", "componenthost/resources", nil, true},
		{"componenthost/resources/listener_intent.go", "componenthost/resources", nil, true},
		{"componenthost/resources/management.go", "componenthost/resources", nil, true},
		{"componenthost/resources/socket_coverage.go", "componenthost/resources", nil, true},
		{"componenthost/deploymentidentity/expected.go", "componenthost/deploymentidentity", map[string]bool{"ExpectedApplicationOwnerV1": true}, false},
		{"componenthost/fallbacktargets/contracts_v2.go", "componenthost/fallbacktargets", map[string]bool{"FallbackTargetReferenceV2": true}, false},
		{"network/intent/protection.go", "network/intent", nil, true},
	} {
		t.Run(entry.file, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), filepath.FromSlash(entry.file)), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			imports := map[string]string{}
			for _, spec := range file.Imports {
				value, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				name := filepath.Base(value)
				if spec.Name != nil {
					name = spec.Name.Name
				}
				imports[name] = value
			}
			owner := "github.com/MalenkiySolovey/solovey-host/" + entry.owner
			count := 0
			for _, declaration := range file.Decls {
				switch value := declaration.(type) {
				case *ast.GenDecl:
					for _, spec := range value.Specs {
						typ, ok := spec.(*ast.TypeSpec)
						if !ok || entry.selected != nil && !entry.selected[typ.Name.Name] {
							continue
						}
						count++
						selected, ok := typ.Type.(*ast.SelectorExpr)
						if !ok || !typ.Assign.IsValid() {
							t.Fatalf("%s has a second editable definition", typ.Name.Name)
						}
						qualifier, ok := selected.X.(*ast.Ident)
						if !ok || imports[qualifier.Name] != owner || selected.Sel.Name != typ.Name.Name {
							t.Fatalf("%s does not alias its public owner", typ.Name.Name)
						}
					}
				case *ast.FuncDecl:
					if !entry.forwardFunctions || !ast.IsExported(value.Name.Name) {
						continue
					}
					if value.Recv != nil || value.Body == nil || len(value.Body.List) != 1 {
						t.Fatalf("%s contains a local contract implementation", value.Name.Name)
					}
					returned, ok := value.Body.List[0].(*ast.ReturnStmt)
					if !ok || len(returned.Results) != 1 {
						t.Fatalf("%s is not a narrow forwarder", value.Name.Name)
					}
					call, ok := returned.Results[0].(*ast.CallExpr)
					if !ok {
						t.Fatal("contract forwarder does not call its public owner")
					}
					selected, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						t.Fatal("contract forwarder calls a private implementation")
					}
					qualifier, ok := selected.X.(*ast.Ident)
					if !ok || imports[qualifier.Name] != owner || selected.Sel.Name != value.Name.Name {
						t.Fatalf("%s forwards to another authority", value.Name.Name)
					}
				}
			}
			if entry.selected != nil && count != len(entry.selected) {
				t.Fatal("required public aliases disappeared")
			}
			if count == 0 && entry.file != "componenthost/resources/fingerprint.go" {
				t.Fatal("public alias facade disappeared")
			}
		})
	}
}
