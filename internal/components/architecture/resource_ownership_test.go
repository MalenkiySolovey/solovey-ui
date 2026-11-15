package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

func TestResourceCapabilityHasOneSemanticApplicationOwner(t *testing.T) {
	// The facade alias is enforced separately without importing Product owners
	// into this pure architecture package. Inspect the actual public definition.
	resourceType := reflect.TypeOf(canonical.ProtectableResourceCapabilities{})
	fields := map[string]int{}
	for index := 0; index < resourceType.NumField(); index++ {
		fields[resourceType.Field(index).Name]++
	}
	if fields["ExpectedApplicationOwner"] != 1 {
		t.Fatalf("generic resource capability has %d expected application owners", fields["ExpectedApplicationOwner"])
	}
	for field := range fields {
		lower := strings.ToLower(field)
		if field != "ExpectedApplicationOwner" && (strings.Contains(lower, "expected") && strings.Contains(lower, "owner") ||
			strings.Contains(lower, "systemd") || strings.Contains(lower, "procd") || strings.Contains(lower, "openwrt")) {
			t.Errorf("generic resource capability retains supervisor-specific owner field %s", field)
		}
	}
}

func TestGenericOwnerConsumersDoNotSelectInstalledSupervisorSchemas(t *testing.T) {
	root := moduleRoot(t)
	forbiddenIdentifiers := map[string]bool{
		"LoadInstalled": true, "LoadProcdInstalled": true,
		"ApplicationOwnerContractV1": true, "ApplicationOwnerContractProcdV1": true,
		"ExpectedListenerOwner": true, "ExpectedOpenWrtListenerOwner": true,
	}
	var violations []string
	for _, relative := range []string{"componenthost/resources", "service/resourceinventory"} {
		walkGoFiles(t, filepath.Join(root, filepath.FromSlash(relative)), func(path string) {
			if strings.HasSuffix(path, "_test.go") {
				return
			}
			for _, name := range sourceIdentifiers(t, path) {
				if forbiddenIdentifiers[name] {
					violations = append(violations, filepath.ToSlash(mustRel(t, root, path))+" names "+name)
				}
			}
		})
	}
	graph := protectionSourcePath(t, "service/resources/graph.go")
	for _, imported := range fileImports(t, graph) {
		if strings.Contains(imported, "/deploymentidentity") || strings.Contains(imported, "/runtimecontract") {
			violations = append(violations, "Server Protection semantic graph imports "+imported)
		}
	}
	for _, name := range sourceIdentifiers(t, graph) {
		if forbiddenIdentifiers[name] || name == "SystemdUnit" || name == "ProcdService" || name == "ProcdInstance" {
			violations = append(violations, "Server Protection semantic graph names "+name)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("generic owner boundary violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestListenerProofRequestIsSupervisorNeutral(t *testing.T) {
	fields := namedStructFields(t,
		protectionSourcePath(t, "service/helpercontract/protocol.go"),
		"ListenerOwnerObserveRequest")
	for field := range fields {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "systemd") || strings.Contains(lower, "procd") || strings.Contains(lower, "supervisor") {
			t.Errorf("listener proof request exposes supervisor selector %s", field)
		}
	}
}

func TestHelperFacadeAliasesTheCanonicalOperationDeclarations(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "components", "server-protection", "service", "helper", "protocol.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	canonicalImport := false
	for _, imported := range file.Imports {
		if imported.Name != nil && imported.Name.Name == "canonical" && imported.Path.Value == `"`+protectionSourceModule+`/service/helpercontract"` {
			canonicalImport = true
		}
	}
	if !canonicalImport {
		t.Fatal("helper facade does not bind the canonical declarations")
	}
	found := map[string]bool{}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.TypeSpec)
			if !ok || !ast.IsExported(value.Name.Name) {
				continue
			}
			selector, ok := value.Type.(*ast.SelectorExpr)
			if !ok || !value.Assign.IsValid() || selector.Sel.Name != value.Name.Name {
				t.Fatalf("helper facade gained an editable declaration: %s", value.Name.Name)
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok || owner.Name != "canonical" {
				t.Fatalf("helper facade changed its declaration owner: %s", value.Name.Name)
			}
			found[value.Name.Name] = true
		}
	}
	for _, required := range []string{"Request", "Response", "ListenerOwnerObserveRequest", "SSHRecoveryObserveRequest"} {
		if !found[required] {
			t.Fatalf("canonical helper alias missing: %s", required)
		}
	}
}

func namedStructFields(t *testing.T, path, typeName string) map[string]int {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]int{}
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range generic.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != typeName {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s is not a struct", typeName)
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					result[name.Name]++
				}
			}
			return result
		}
	}
	t.Fatalf("struct %s not found in %s", typeName, path)
	return nil
}

func sourceIdentifiers(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	ast.Inspect(file, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			result = append(result, identifier.Name)
		}
		return true
	})
	return result
}
