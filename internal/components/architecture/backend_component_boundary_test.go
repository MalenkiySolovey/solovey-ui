package architecture

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestComponentHTTPAndActionOwnersHaveIndependentTransitiveClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{"backend/actions", "backend/audit", "backend/componentapi", "backend/componentview", "backend/security/uuid"} {
		allowed[backendModulePath+"/"+strings.TrimPrefix(name, "backend/")] = true
	}
	for _, target := range []string{"github.com/gin-gonic/gin", "github.com/gofrs/uuid/v5", "github.com/MalenkiySolovey/solovey-host/operationref", "github.com/MalenkiySolovey/solovey-host/componenthost/manifest", "github.com/MalenkiySolovey/solovey-host/auditrecord"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{backendModulePath + "/actions", backendModulePath + "/componentapi", backendModulePath + "/componentview"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Errorf("%s imports a Product runtime/lifecycle/storage owner: %s", target, strings.Join(violations, ", "))
		}
	}
}

func TestComponentHTTPUsesOneBackendOwnerAndNarrowSettingsCommand(t *testing.T) {
	root := moduleRoot(t)
	for _, retired := range []string{"components/panel-update-ui/api", "components/panel-update-ui/service", "componenthost/actions"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(retired))); err == nil {
			entries, readErr := os.ReadDir(filepath.Join(root, filepath.FromSlash(retired)))
			if readErr != nil || len(entries) > 0 {
				t.Errorf("retired editable owner remains at %s", retired)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	component := filepath.Join(root, "components", "panel-update-ui", "component.go")
	for _, imported := range fileImports(t, component) {
		if imported == modulePath+"/service" {
			t.Fatal("Panel Update HTTP still imports the broad Panel service")
		}
	}
	data, err := os.ReadFile(component)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "host.Runtime") {
		t.Fatal("Panel Update route composition still consumes Runtime")
	}
}
