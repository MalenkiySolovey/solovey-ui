package architecture

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/MalenkiySolovey/solovey-ui"

func TestComponentImportsStayBehindCompositionPackages(t *testing.T) {
	root := moduleRoot(t)
	prefix := modulePath + "/components/"
	var violations []string
	walkProductGoFiles(t, root, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, imported := range fileImports(t, path) {
			ownedImport := componentSourceImport(imported)
			if !strings.HasPrefix(ownedImport, prefix) || componentImportAllowed(rel, ownedImport) {
				continue
			}
			violations = append(violations, rel+" imports "+imported)
		}
	})
	if len(violations) > 0 {
		t.Fatalf("component import boundary violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestInternalComponentsStayPure(t *testing.T) {
	root := moduleRoot(t)
	forbidden := []string{modulePath + "/service", modulePath + "/database", modulePath + "/componenthost"}
	assertNoImports(t, root, []string{filepath.Join("internal", "components")}, forbidden, "internal component purity")
}

func TestComponentKitIsNotImportedByCore(t *testing.T) {
	root := moduleRoot(t)
	prefix := modulePath + "/componentkit/"
	var violations []string
	walkProductGoFiles(t, root, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		if strings.HasPrefix(rel, "components/") || strings.HasPrefix(rel, "componentkit/") {
			return
		}
		for _, imported := range fileImports(t, path) {
			if strings.HasPrefix(imported, prefix) {
				violations = append(violations, rel+" imports "+imported)
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("componentkit must stay out of core packages:\n%s", strings.Join(violations, "\n"))
	}
}

func TestComponentKitHasNoRuntimeOwnership(t *testing.T) {
	root := moduleRoot(t)
	kitRoot := filepath.Join(root, "componentkit")
	forbiddenImports := []string{modulePath + "/componenthost", modulePath + "/cronjob", modulePath + "/database", modulePath + "/logger", modulePath + "/service", "github.com/robfig/cron/v3", "gorm.io/gorm"}
	forbiddenLifecycle := map[string]bool{"Start": true, "Stop": true, "Migrate": true, "DropData": true, "Register": true, "Unregister": true, "Reconcile": true}
	var violations []string
	walkGoFiles(t, kitRoot, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		if strings.HasSuffix(rel, "_test.go") {
			return
		}
		for _, imported := range fileImports(t, path) {
			for _, forbidden := range forbiddenImports {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					violations = append(violations, rel+" imports runtime owner "+imported)
				}
			}
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if declaration, ok := node.(*ast.FuncDecl); ok && forbiddenLifecycle[declaration.Name.Name] {
				violations = append(violations, rel+" declares lifecycle function "+declaration.Name.Name)
			}
			return true
		})
	})
	if len(violations) > 0 {
		t.Fatalf("componentkit owns runtime behavior:\n%s", strings.Join(violations, "\n"))
	}
}

func TestComponentsDoNotImportCoreTransportOrComposition(t *testing.T) {
	root := moduleRoot(t)
	assertNoImports(t, root, []string{"components"}, []string{modulePath + "/api", modulePath + "/app", modulePath + "/cmd", modulePath + "/sub", modulePath + "/web"}, "component transport/composition")
}

func TestProductionPackagesDoNotDependOnTestSupport(t *testing.T) {
	root := moduleRoot(t)
	prefix := modulePath + "/testsupport"
	var violations []string
	walkProductGoFiles(t, root, func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, imported := range fileImports(t, path) {
			if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
				violations = append(violations, rel+" imports "+imported)
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("production packages depend on test support:\n%s", strings.Join(violations, "\n"))
	}
}

func TestComponentDatabaseResourcesHaveUniqueOwners(t *testing.T) {
	root := moduleRoot(t)
	_, resourcesByOwner := componentDatabaseResources(t, root)
	resourceOwner := map[string]string{}
	for owner, resources := range resourcesByOwner {
		for _, resource := range resources {
			if previous := resourceOwner[resource]; previous != "" {
				t.Fatalf("database resource %q is declared by both %s and %s", resource, previous, owner)
			}
			resourceOwner[resource] = owner
		}
	}
}

func TestRetiredServerProtectionMutationSurfacesStayAbsent(t *testing.T) {
	root := moduleRoot(t)
	retired := []string{"components/server-protection/cmd/solovey-protect-helper", "components/server-protection/service/classifier", "components/server-protection/service/handoff", "components/server-protection/service/recoverypath", "components/server-protection/service/helper/process.go", "components/server-protection/service/repository/port_operations.go"}
	for _, relative := range retired {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Errorf("retired mutation surface exists: %s", relative)
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("retired mutation surface is nonempty: %s", relative)
		}
	}
	routes, err := os.ReadFile(filepath.Join(root, "components", "server-protection", "api", "routes.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/ports/prepare", "/ports/apply", "/ports/rollback"} {
		if bytes.Contains(routes, []byte(route)) {
			t.Errorf("retired mutation route exists: %s", route)
		}
	}
}

func componentSourceImport(imported string) string {
	const external = "github.com/MalenkiySolovey/solovey-component-"
	if strings.HasPrefix(imported, external) {
		return modulePath + "/components/" + strings.TrimPrefix(imported, external)
	}
	return imported
}

func TestExternalComponentImportsPreserveSemanticOwnership(t *testing.T) {
	const source = "github.com/MalenkiySolovey/solovey-component-telegram"
	const observations = "github.com/MalenkiySolovey/solovey-component-observability-extra"
	for _, fixture := range []struct {
		path, imported string
		allowed        bool
	}{
		{"components/paid-subscriptions/telegram/bot.go", source, false},
		{"components/paid-subscriptions/telegram/bot.go", source + "/service", false},
		{"service/notifications.go", source + "/service", false},
		{"componentkit/telegram/transport.go", source + "/service", false},
		{"components/telegram/component.go", source, true},
		{"components/telegram/service/delivery.go", source + "/service", true},
		{"app/components_generated.go", source, true},
		{"components/observability-extra/component.go", observations, true},
		{"service/observability/service.go", observations + "/history", true},
		{"service/observability/types.go", observations + "/history", true},
		{"service/diagnostics.go", observations + "/history", false},
		{"service/server/service.go", observations + "/history", false},
		{"componenthost/health/registry.go", observations + "/history", false},
		{"components/paid-subscriptions/service.go", observations + "/history", false},
	} {
		if got := componentImportAllowed(fixture.path, componentSourceImport(fixture.imported)); got != fixture.allowed {
			t.Errorf("%s importing %s: allowed=%v, want %v", fixture.path, fixture.imported, got, fixture.allowed)
		}
	}
}

func TestTelegramFeatureSourceRetirement(t *testing.T) {
	for _, relative := range []string{
		"components/telegram/frontend/index.ts", "components/telegram/frontend/views/TelegramSettings.vue",
		"components/telegram/jobs/cpu_alert.go", "components/telegram/jobs/telegram_report.go", "components/telegram/jobs/telegram_backup.go",
		"components/telegram/service/botapi.go", "components/telegram/service/discovery.go", "components/telegram/service/notification.go",
	} {
		if _, err := os.Stat(filepath.Join(moduleRoot(t), filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Errorf("retired editable feature source returned: %s (stat: %v)", relative, err)
		}
	}
}

func TestObservationFeatureHasOneEditableImplementation(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{
		"components/observability-extra/jobs/sampling.go", "components/observability-extra/jobs/sampling_test.go",
		"components/observability-extra/frontend/index.ts", "components/observability-extra/frontend/views/Audit.vue",
		"components/observability-extra/frontend/views/Diagnostics.vue", "components/observability-extra/frontend/composables/useDiagnosticsPage.ts",
		"service/observability/aggregate.go", "service/observability/service_test.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Errorf("retired editable history or UI returned: %s (%v)", relative, err)
		}
	}
	// The compatibility facade binds state/reset and forwards queries. It cannot
	// grow another sampling, aggregation or ring algorithm under a different name.
	walkGoFiles(t, filepath.Join(root, "service", "observability"), func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				t.Errorf("history compatibility facade contains algorithm loop: %s", path)
			}
			return true
		})
	})
}

func componentImportAllowed(rel, imported string) bool {
	// Only the existing local-host compatibility facade may bind optional history.
	// Current facts, Diagnostics, Health and other components retain their owners.
	if imported == modulePath+"/components/observability-extra/history" &&
		(rel == "service/observability/service.go" || rel == "service/observability/types.go") {
		return true
	}
	if strings.HasPrefix(rel, "app/") || strings.HasPrefix(rel, "cmd/") {
		return true
	}
	if !strings.HasPrefix(rel, "components/") {
		return false
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return false
	}
	prefix := modulePath + "/components/" + parts[1]
	return imported == prefix || strings.HasPrefix(imported, prefix+"/")
}

func assertNoImports(t *testing.T, root string, relativeRoots, forbidden []string, boundary string) {
	t.Helper()
	var violations []string
	for _, relativeRoot := range relativeRoots {
		walkGoFiles(t, filepath.Join(root, relativeRoot), func(path string) {
			rel := filepath.ToSlash(mustRel(t, root, path))
			for _, imported := range fileImports(t, path) {
				for _, prefix := range forbidden {
					if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
						violations = append(violations, rel+" imports "+imported)
					}
				}
			}
		})
	}
	if len(violations) > 0 {
		t.Fatalf("%s boundary violated:\n%s", boundary, strings.Join(violations, "\n"))
	}
}

type componentResourceManifest struct {
	ID       string `json:"id"`
	Database struct {
		Tables   []string `json:"tables"`
		Settings []string `json:"settings"`
		Secrets  []string `json:"secrets"`
	} `json:"database"`
}

func componentDatabaseResources(t *testing.T, root string) (map[string]string, map[string][]string) {
	t.Helper()
	componentRoots := map[string]string{}
	resourcesByOwner := map[string][]string{}
	entries, err := os.ReadDir(filepath.Join(root, "components"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(root, "components", entry.Name(), "component.json")
		content, err := os.ReadFile(manifestPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var manifest componentResourceManifest
		if err := json.Unmarshal(content, &manifest); err != nil || manifest.ID == "" || manifest.ID != entry.Name() {
			t.Fatalf("component directory %s has invalid manifest id %q: %v", entry.Name(), manifest.ID, err)
		}
		componentRoots[manifest.ID] = filepath.Join(root, "components", entry.Name())
		resources := append([]string(nil), manifest.Database.Tables...)
		resources = append(resources, manifest.Database.Settings...)
		resources = append(resources, manifest.Database.Secrets...)
		resourcesByOwner[manifest.ID] = resources
	}
	return componentRoots, resourcesByOwner
}

func coreSourceRoots() []string {
	return []string{"api", "app", "cmd", "componenthost", "config", "core", "database", "deploy", filepath.Join("frontend", "src"), "internal", "ipmonitor", "logger", "middleware", "realtime", "service", "sub", "util", "web"}
}

func walkProductGoFiles(t *testing.T, root string, visit func(string)) {
	t.Helper()
	for _, relativeRoot := range append(coreSourceRoots(), "componentkit", "components") {
		walkGoFiles(t, filepath.Join(root, relativeRoot), visit)
	}
}

func walkGoFiles(t *testing.T, root string, visit func(string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			visit(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func fileImports(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, item := range file.Imports {
		imports = append(imports, strings.Trim(item.Path.Value, `"`))
	}
	return imports
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".runtime", ".gotmp", "bin", "dist", "node_modules", "playwright-report", "test-results":
		return true
	default:
		return false
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found")
		}
		directory = parent
	}
}

func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}
