package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
		if strings.HasSuffix(rel, "_test.go") {
			return
		}
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
		if strings.HasSuffix(rel, "_test.go") {
			return
		}
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

func TestCoreSourceDoesNotNameConcreteOptionalComponents(t *testing.T) {
	root := moduleRoot(t)
	forbiddenTokens := []string{
		"import-xui",
		"observability-extra",
		"ObservabilityExtra",
		"RegisterObservabilityExtra",
		"panel-update-ui",
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

func componentOwnsImport(rel string, imported string) bool {
	relParts := strings.Split(rel, "/")
	if len(relParts) < 2 || relParts[0] != "components" {
		return false
	}
	componentID := relParts[1]
	return strings.HasPrefix(imported, modulePath+"/components/"+componentID)
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

func allowedComponentKnowledgeFile(rel string) bool {
	if strings.HasPrefix(rel, "app/components_") {
		return true
	}
	if strings.HasPrefix(rel, "cmd/optional_commands_") {
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
