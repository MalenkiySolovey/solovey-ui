package architecture

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const modulePath = "github.com/MalenkiySolovey/solovey-ui"

func TestComponentImportsStayBehindCompositionRoot(t *testing.T) {
	root := moduleRoot(t)
	componentImportPrefix := modulePath + "/components/"

	var violations []string
	walkGoFiles(t, root, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, imported := range fileImports(t, path) {
			if !strings.HasPrefix(imported, componentImportPrefix) {
				continue
			}
			if strings.HasPrefix(rel, "app/components_") {
				continue
			}
			if strings.HasPrefix(rel, "cmd/optional_commands_") {
				continue
			}
			if strings.HasPrefix(rel, "cmd/solovey-privileged-broker/components_") {
				continue
			}
			if strings.HasPrefix(rel, "components/") {
				if componentOwnsImport(rel, imported) {
					continue
				}
				violations = append(violations, rel+" imports sibling component "+imported)
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
	internalComponentsRoot := filepath.Join(root, "internal", "components")
	forbiddenImports := []string{
		modulePath + "/service",
		modulePath + "/database",
		modulePath + "/componenthost",
	}

	var violations []string
	walkGoFiles(t, internalComponentsRoot, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, imported := range fileImports(t, path) {
			for _, forbidden := range forbiddenImports {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					violations = append(violations, rel+" imports "+imported)
				}
			}
		}
	})

	if len(violations) > 0 {
		t.Fatalf("internal component purity violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestComponentKitIsNotImportedByCore(t *testing.T) {
	root := moduleRoot(t)
	componentKitImportPrefix := modulePath + "/componentkit/"

	var violations []string
	walkGoFiles(t, root, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		if strings.HasPrefix(rel, "components/") || strings.HasPrefix(rel, "componentkit/") {
			return
		}
		for _, imported := range fileImports(t, path) {
			if strings.HasPrefix(imported, componentKitImportPrefix) {
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
	forbiddenImports := []string{
		modulePath + "/componenthost",
		modulePath + "/cronjob",
		modulePath + "/database",
		modulePath + "/logger",
		modulePath + "/service",
		"github.com/robfig/cron/v3",
		"gorm.io/gorm",
	}
	forbiddenLifecycleNames := map[string]struct{}{
		"Start": {}, "Stop": {}, "Migrate": {}, "DropData": {},
		"Register": {}, "Unregister": {}, "Reconcile": {},
	}

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
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			declaration, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			if _, forbidden := forbiddenLifecycleNames[declaration.Name.Name]; forbidden {
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
	forbidden := []string{
		modulePath + "/api",
		modulePath + "/app",
		modulePath + "/cmd",
		modulePath + "/sub",
		modulePath + "/web",
	}
	var violations []string
	walkGoFiles(t, filepath.Join(root, "components"), func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		for _, imported := range fileImports(t, path) {
			for _, prefix := range forbidden {
				if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
					violations = append(violations, rel+" imports core transport/composition "+imported)
				}
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("component transport/composition boundary violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestComponentsDoNotNameSiblingOwnedDatabaseResources(t *testing.T) {
	root := moduleRoot(t)
	componentRoots, resourcesByOwner := componentDatabaseResources(t, root)
	resourceOwner := map[string]string{}
	for owner, resources := range resourcesByOwner {
		for _, resource := range resources {
			if previous, exists := resourceOwner[resource]; exists {
				t.Fatalf("database resource %q is declared by both %s and %s", resource, previous, owner)
			}
			resourceOwner[resource] = owner
		}
	}

	var violations []string
	for componentID, componentRoot := range componentRoots {
		walkComponentSourceFiles(t, componentRoot, true, func(path string) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for resource, owner := range resourceOwner {
				if owner != componentID && strings.Contains(string(content), resource) {
					rel := filepath.ToSlash(mustRel(t, root, path))
					violations = append(violations, rel+" names "+owner+" resource "+resource)
				}
			}
		})
	}
	if len(violations) > 0 {
		t.Fatalf("component database ownership boundary violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestComponentsDoNotNameHyphenatedSiblingIDsInProduction(t *testing.T) {
	root := moduleRoot(t)
	componentRoots, _ := componentDatabaseResources(t, root)
	var violations []string
	for componentID, componentRoot := range componentRoots {
		walkComponentSourceFiles(t, componentRoot, false, func(path string) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for siblingID := range componentRoots {
				if siblingID == componentID || !strings.Contains(siblingID, "-") {
					continue
				}
				if strings.Contains(string(content), siblingID) {
					rel := filepath.ToSlash(mustRel(t, root, path))
					violations = append(violations, rel+" names sibling component "+siblingID)
				}
			}
		})
	}
	if len(violations) > 0 {
		t.Fatalf("component semantic boundary violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCoreSourceDoesNotNameConcreteOptionalComponents(t *testing.T) {
	root := moduleRoot(t)
	forbiddenTokens := []string{
		"import-xui",
		"observability-extra",
		"ObservabilityExtra",
		"RegisterObservabilityExtra",
		"panel-update-ui",
		"fallback-html",
		"PanelUpdateService",
		"NewPanelUpdateManager",
		"paid-subscriptions",
		"paidSub",
		"remote-outbound-subscriptions",
		"remoteOutboundSubscriptions",
		"RemoteOutboundService",
		"remoteGroup",
		"remoteSubscription",
		"remoteGroupLinks",
		"subRemoteGroupAdaptation",
		"subRemoteConversionPolicy",
		"TelegramService",
		"telegramBackupPassphrase",
		"server-protection",
		"serverProtection",
		"ServerProtection",
	}

	var violations []string
	walkSourceFiles(t, root, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		if allowedComponentKnowledgeFile(rel) {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, token := range forbiddenTokens {
			if strings.Contains(text, token) {
				violations = append(violations, rel+" contains "+token)
			}
		}
	})

	if len(violations) > 0 {
		t.Fatalf("core source names concrete optional components:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDeprecatedProtectionNamesStayOutOfRuntimeSource(t *testing.T) {
	root := moduleRoot(t)
	forbidden := []string{"inbound-protection", "inbound_protection", "Inbound Protection", "inbound_protection_draft"}
	var violations []string
	walkSourceFiles(t, root, func(path string) {
		rel := filepath.ToSlash(mustRel(t, root, path))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, token := range forbidden {
			if strings.Contains(string(data), token) {
				violations = append(violations, rel+" contains deprecated "+token)
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("deprecated protection names remain in runtime source:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRuntimeSourceDoesNotContainDevelopmentProvenance(t *testing.T) {
	root := moduleRoot(t)
	forbidden := []string{
		"C:\\Users\\",
		"ChatGPT",
		"Claude",
		"Codex",
		"Gemini",
		"QCOW",
		"QEMU",
		"SUI_TEST_LAB",
		"cloud-init",
		"solovey-lab",
		"sui-lab",
		"tools/lab",
		"tools\\lab",
	}
	deprecatedMilestone := regexp.MustCompile(`(?i)\b(?:phase[\s_-]*1[578]|p1[578][-_])`)
	var violations []string
	walkProductionSourceFiles(t, root, func(path string) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, token := range forbidden {
			if strings.Contains(string(content), token) {
				rel := filepath.ToSlash(mustRel(t, root, path))
				violations = append(violations, rel+" contains development provenance "+token)
			}
		}
		if marker := deprecatedMilestone.FindString(string(content)); marker != "" {
			rel := filepath.ToSlash(mustRel(t, root, path))
			violations = append(violations, rel+" contains a development milestone identifier")
		}
	})
	if len(violations) > 0 {
		t.Fatalf("development provenance leaked into runtime source:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRetiredServerProtectionSurfacesStayAbsent(t *testing.T) {
	root := moduleRoot(t)
	retired := []string{
		"components/server-protection/cmd/solovey-protect-helper",
		"components/server-protection/service/classifier",
		"components/server-protection/service/handoff",
		"components/server-protection/service/recoverypath",
		"components/server-protection/service/helper/process.go",
		"components/server-protection/service/repository/port_operations.go",
	}
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
			t.Errorf("retired server-protection surface exists: %s", relative)
			continue
		}
		err = filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				t.Errorf("retired server-protection surface exists: %s", filepath.ToSlash(mustRel(t, root, candidate)))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	routes, err := os.ReadFile(filepath.Join(root, "components", "server-protection", "api", "routes.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/ports/prepare", "/ports/apply", "/ports/rollback"} {
		if bytes.Contains(routes, []byte(route)) {
			t.Errorf("retired server-protection route exists: %s", route)
		}
	}
}

func componentOwnsImport(rel string, imported string) bool {
	relParts := strings.Split(rel, "/")
	if len(relParts) < 2 || relParts[0] != "components" {
		return false
	}
	componentID := relParts[1]
	componentPrefix := modulePath + "/components/" + componentID
	return imported == componentPrefix || strings.HasPrefix(imported, componentPrefix+"/")
}

func walkSourceFiles(t *testing.T, root string, visit func(path string)) {
	t.Helper()
	allowedExt := map[string]struct{}{
		".go":  {},
		".ts":  {},
		".vue": {},
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(entry.Name())
		if _, ok := allowedExt[ext]; !ok {
			return nil
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		if strings.HasSuffix(rel, "_test.go") ||
			strings.HasSuffix(rel, ".test.ts") ||
			strings.HasSuffix(rel, ".spec.ts") ||
			strings.HasPrefix(rel, "components/") ||
			strings.HasPrefix(rel, "componentkit/") {
			return nil
		}
		visit(path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
	componentsRoot := filepath.Join(root, "components")
	entries, err := os.ReadDir(componentsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(componentsRoot, entry.Name(), "component.json")
		content, err := os.ReadFile(manifestPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", manifestPath, err)
		}
		var manifest componentResourceManifest
		if err := json.Unmarshal(content, &manifest); err != nil {
			t.Fatalf("parse %s: %v", manifestPath, err)
		}
		if manifest.ID == "" || manifest.ID != entry.Name() {
			t.Fatalf("component directory %s has manifest id %q", entry.Name(), manifest.ID)
		}
		componentRoots[manifest.ID] = filepath.Join(componentsRoot, entry.Name())
		resources := append([]string(nil), manifest.Database.Tables...)
		resources = append(resources, manifest.Database.Settings...)
		resources = append(resources, manifest.Database.Secrets...)
		resourcesByOwner[manifest.ID] = resources
	}
	return componentRoots, resourcesByOwner
}

func walkComponentSourceFiles(t *testing.T, root string, includeTests bool, visit func(path string)) {
	t.Helper()
	allowedExt := map[string]struct{}{".go": {}, ".sql": {}, ".ts": {}, ".vue": {}}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := allowedExt[filepath.Ext(entry.Name())]; !ok {
			return nil
		}
		if !includeTests && (strings.HasSuffix(entry.Name(), "_test.go") || strings.HasSuffix(entry.Name(), ".test.ts") || strings.HasSuffix(entry.Name(), ".spec.ts")) {
			return nil
		}
		visit(path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func walkProductionSourceFiles(t *testing.T, root string, visit func(path string)) {
	t.Helper()
	productionRoots := []string{
		"api", "app", "cmd", "componenthost", "componentkit", "components",
		"config", "core", "database", filepath.Join("frontend", "src"), "internal",
		"ipmonitor", "logger", "middleware", "realtime", "service", "sub", "util", "web",
	}
	allowedExt := map[string]struct{}{".go": {}, ".js": {}, ".ts": {}, ".vue": {}}
	for _, relativeRoot := range productionRoots {
		pathRoot := filepath.Join(root, relativeRoot)
		err := filepath.WalkDir(pathRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if skipDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".spec.ts") {
				return nil
			}
			if _, ok := allowedExt[filepath.Ext(name)]; ok {
				visit(path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func allowedComponentKnowledgeFile(rel string) bool {
	if strings.HasPrefix(rel, "app/components_") {
		return true
	}
	if strings.HasPrefix(rel, "cmd/optional_commands_") {
		return true
	}
	if strings.HasPrefix(rel, "cmd/solovey-privileged-broker/components_") {
		return true
	}
	return false
}

func walkGoFiles(t *testing.T, root string, visit func(path string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
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
		t.Fatalf("parse %s: %v", path, err)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, item := range file.Imports {
		imports = append(imports, strings.Trim(item.Path.Value, `"`))
	}
	return imports
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".runtime", "bin", "dist", "node_modules", "playwright-report", "test-results":
		return true
	default:
		return false
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func mustRel(t *testing.T, root string, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}
