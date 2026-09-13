package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceCapabilityHasOneSemanticApplicationOwner(t *testing.T) {
	root := moduleRoot(t)
	fields := namedStructFields(t, filepath.Join(root, "componenthost", "resources", "types.go"), "ProtectableResourceCapabilities")
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
	graph := filepath.Join(root, "components", "server-protection", "service", "resources", "graph.go")
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
	root := moduleRoot(t)
	fields := namedStructFields(t,
		filepath.Join(root, "components", "server-protection", "service", "helper", "protocol.go"),
		"ListenerOwnerObserveRequest")
	for field := range fields {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "systemd") || strings.Contains(lower, "procd") || strings.Contains(lower, "supervisor") {
			t.Errorf("listener proof request exposes supervisor selector %s", field)
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
