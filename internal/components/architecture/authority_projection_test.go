package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProcessIdentityConsumersDependOnTheSingleNeutralEvidenceProvider(t *testing.T) {
	root := moduleRoot(t)
	providerImport := modulePath + "/internal/ops/processevidence"
	consumers := []string{
		"components/server-protection/service/hostsurface/platform_linux.go",
		"components/server-protection/service/helper/listener_owner_systemd_linux.go",
		"components/server-protection/service/helper/listener_owner_procd_linux.go",
		"internal/ops/privilegedbroker/peer_linux.go",
		"internal/ops/privilegedbroker/supervision_systemd_linux.go",
		"internal/ops/privilegedbroker/supervision_procd_linux.go",
		"internal/ops/sshbroker/host_linux.go",
		"internal/ops/sshbroker/dropbear_uci_linux.go",
		"internal/ops/sshbroker/recovery_linux.go",
	}
	for _, relative := range consumers {
		imports := fileImports(t, filepath.Join(root, filepath.FromSlash(relative)))
		if !containsImport(imports, providerImport) {
			t.Errorf("process identity consumer %s bypasses the neutral evidence provider", relative)
		}
	}

	semanticRoots := []string{
		"components/server-protection/service/hostsurface",
		"components/server-protection/service/helper",
		"internal/ops/privilegedbroker",
		"internal/ops/sshbroker",
	}
	var violations []string
	for _, relativeRoot := range semanticRoots {
		walkGoFiles(t, filepath.Join(root, filepath.FromSlash(relativeRoot)), func(path string) {
			if strings.HasSuffix(path, "_test.go") {
				return
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isOSReadCall(call.Fun) || len(call.Args) == 0 {
					return true
				}
				for _, literal := range expressionStringLiterals(call.Args[0]) {
					if literal == "stat" || literal == "status" || literal == "cgroup" ||
						strings.HasSuffix(filepath.ToSlash(literal), "/stat") || strings.HasSuffix(filepath.ToSlash(literal), "/status") || strings.HasSuffix(filepath.ToSlash(literal), "/cgroup") {
						violations = append(violations, filepath.ToSlash(mustRel(t, root, path))+" reopens process "+literal)
					}
				}
				return true
			})
		})
	}
	if len(violations) > 0 {
		t.Fatalf("semantic consumers reopen process metadata outside processevidence:\n%s", strings.Join(violations, "\n"))
	}
}

func TestSSHClassificationConsumesOnlyTheTypedSemanticProjection(t *testing.T) {
	root := moduleRoot(t)
	checks := []struct {
		path      string
		function  string
		forbidden map[string]bool
	}{
		{"components/server-protection/service/hostsurface/provider.go", "matchSSHProjection", map[string]bool{"Process": true, "Service": true, "Executable": true, "SystemdUnit": true, "ProcdService": true, "ProcdInstance": true}},
		{"componenthost/management/inventory.go", "IsSSHSurface", map[string]bool{"Process": true, "Service": true, "Executable": true, "SystemdUnit": true, "ProcdService": true, "ProcdInstance": true}},
	}
	for _, check := range checks {
		path := filepath.Join(root, filepath.FromSlash(check.path))
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		function := findFunction(file, check.function)
		if function == nil {
			t.Fatalf("typed SSH projection consumer %s.%s is absent", check.path, check.function)
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && check.forbidden[selector.Sel.Name] {
				t.Errorf("%s.%s reopens private SSH taxonomy through %s", check.path, check.function, selector.Sel.Name)
			}
			return true
		})
	}
}

func TestFeaturePreviewAndPlanningConsumeNarrowCapabilityTypes(t *testing.T) {
	root := moduleRoot(t)
	checks := []struct {
		path, structure, field, typeName string
	}{
		{"components/server-protection/service/firewall/types.go", "PreviewOptions", "NFTCapability", "NFTPreviewCapability"},
		{"components/server-protection/service/interception/service.go", "Service", "KernelCapability", "KernelInterceptionCapabilityV1"},
		{"components/server-protection/service/fronting/nginx.go", "NginxConfig", "ProbeCapability", "Availability"},
	}
	for _, check := range checks {
		path := filepath.Join(root, filepath.FromSlash(check.path))
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !structFieldHasType(file, check.structure, check.field, check.typeName) {
			t.Errorf("%s.%s.%s is not the narrow %s projection", check.path, check.structure, check.field, check.typeName)
		}
		for _, imported := range fileImports(t, path) {
			if imported == "runtime" {
				t.Errorf("narrow capability consumer %s imports runtime OS detection", check.path)
			}
		}
	}
}

func containsImport(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func isOSReadCall(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "os" && (selector.Sel.Name == "ReadFile" || selector.Sel.Name == "Open")
}

func expressionStringLiterals(expression ast.Expr) []string {
	result := []string{}
	ast.Inspect(expression, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil {
			result = append(result, value)
		}
		return true
	})
	return result
}

func findFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}

func structFieldHasType(file *ast.File, structure, field, typeName string) bool {
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range generic.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != structure {
				continue
			}
			value, structType := typeSpec.Type.(*ast.StructType)
			if !structType {
				continue
			}
			for _, candidate := range value.Fields.List {
				identifier, direct := candidate.Type.(*ast.Ident)
				for _, name := range candidate.Names {
					if direct && name.Name == field && identifier.Name == typeName {
						return true
					}
				}
			}
		}
	}
	return false
}
