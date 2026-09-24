package fronting

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkflowV2ReusesRestrictedEngineAndHasNoPublicOrDynamicDestinationSurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	directory := filepath.Dir(file)
	files := []string{filepath.Join(directory, "workflow_v2.go"), filepath.Join(directory, "fixed_l4_v2.go")}
	for _, path := range files {
		parsed := parseArchitectureFile(t, path)
		assertNoForbiddenImports(t, parsed, path, map[string]bool{"os/exec": true, "syscall": true})
		assertNoNetControlAuthority(t, parsed, path)
		assertNoForbiddenIdentifiers(t, parsed, path, map[string]bool{"RawConfig": true, "RawSnippet": true})
	}
	assertAnyIdentifiers(t, files, []string{"OperationNginxValidate", "OperationNginxInstall", "OperationNginxSwitch", "OperationNginxReload", "OperationNginxVerify", "OperationNginxRestore", "Acquire", "MarkMutation", "VerifyRevision"})
	apiPath := filepath.Join(directory, "..", "..", "api", "fronting.go")
	api := parseArchitectureFile(t, apiPath)
	assertNoForbiddenIdentifiers(t, api, apiPath, map[string]bool{"PrepareV2": true, "ApplyV2": true})
	assertAnyIdentifiers(t, []string{apiPath}, []string{"frontingSemanticService", "FrontingStrategyPlanV2", "frontingApplyConfigured"})
}

func TestSNIPrereadRendererHasNoSecondEngineOrDynamicActionSurface(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	directory := filepath.Dir(file)
	files := []string{"sni_preread_v2.go", "workflow_candidate_v2.go", "target_authority_v2.go", "sni_health_v2.go"}
	parsedFiles := make([]*ast.File, 0, len(files))
	for _, name := range files {
		parsed := parseArchitectureFile(t, filepath.Join(directory, name))
		assertNoForbiddenImports(t, parsed, name, map[string]bool{"os/exec": true, "syscall": true})
		assertNoNetControlAuthority(t, parsed, name)
		parsedFiles = append(parsedFiles, parsed)
	}
	assertAnyIdentifiers(t, filesInDirectory(directory, files), []string{"RenderSNIPrereadCandidateV2", "renderWorkflowCandidateV2", "EndpointLeaseProviderV1", "ProviderV2", "SNIPrereadHealthCheckV2"})
	for _, parsed := range parsedFiles {
		assertNoForbiddenIdentifiers(t, parsed, "SNI workflow", map[string]bool{"AppliedActionV1": true, "ActionMap": true})
	}
	if !hasStringLiteral(parsedFiles, "ssl_preread on;") {
		t.Fatal("SNI workflow integration lost ssl_preread directive")
	}
}

func assertNoNetControlAuthority(t *testing.T, file *ast.File, path string) {
	t.Helper()
	netAliases := map[string]bool{}
	for _, imported := range file.Imports {
		if imported.Path.Value != `"net"` {
			continue
		}
		alias := "net"
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		if alias == "." {
			t.Fatalf("%s dot-imports net and obscures network authority", path)
		}
		netAliases[alias] = true
	}
	allowed := map[string]bool{"JoinHostPort": true, "ParseIP": true, "ParseCIDR": true, "SplitHostPort": true, "IP": true, "IPNet": true, "IPAddr": true}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && netAliases[identifier.Name] && !allowed[selector.Sel.Name] {
			t.Fatalf("%s uses net.%s outside the read-only address parser/renderer contract", path, selector.Sel.Name)
		}
		return true
	})
}

func parseArchitectureFile(t *testing.T, path string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func assertNoForbiddenImports(t *testing.T, file *ast.File, path string, forbidden map[string]bool) {
	t.Helper()
	for _, imported := range file.Imports {
		value := imported.Path.Value[1 : len(imported.Path.Value)-1]
		if forbidden[value] {
			t.Fatalf("%s imports forbidden dependency %q", path, value)
		}
	}
}

func assertNoForbiddenIdentifiers(t *testing.T, file *ast.File, path string, forbidden map[string]bool) {
	t.Helper()
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && forbidden[identifier.Name] {
			t.Fatalf("%s contains forbidden architecture identifier %q", path, identifier.Name)
		}
		return true
	})
}

func assertAnyIdentifiers(t *testing.T, paths []string, required []string) {
	t.Helper()
	found := make(map[string]bool, len(required))
	for _, path := range paths {
		file := parseArchitectureFile(t, path)
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok {
				found[identifier.Name] = true
			}
			return true
		})
	}
	for _, name := range required {
		if !found[name] {
			t.Fatalf("architecture contract lost identifier %q", name)
		}
	}
}

func filesInDirectory(directory string, names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(directory, name))
	}
	return paths
}

func hasStringLiteral(files []*ast.File, want string) bool {
	found := false
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if ok && literal.Kind == gotoken.STRING && len(literal.Value) >= 2 && literal.Value[1:len(literal.Value)-1] == want {
				found = true
			}
			return true
		})
	}
	return found
}
