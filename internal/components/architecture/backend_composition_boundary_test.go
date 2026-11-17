package architecture

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestComponentRouteInputHasNoProductOwnerClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{"backend/componentroutes", "backend/componentview", "backend/actions", "backend/security/clientidentity"} {
		allowed[backendModulePath+"/"+strings.TrimPrefix(name, "backend/")] = true
	}
	for _, target := range []string{"github.com/gin-gonic/gin", "github.com/MalenkiySolovey/solovey-host/componenthost", "github.com/MalenkiySolovey/solovey-host/operationref", "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/componentroutes") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Component HTTP input imports Product implementation: %s", strings.Join(violations, ", "))
	}
}

func TestComponentHTTPCompositionUsesExplicitProductInputs(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{"api/component_routes.go", "api/component_owners.go", "api/action_policy.go", "api/config/panel_data.go", "api/operations_status_routes.go", "web/web.go"} {
		for _, imported := range fileImports(t, filepath.Join(root, filepath.FromSlash(relative))) {
			if strings.HasPrefix(imported, modulePath+"/composition/") || imported == modulePath+"/componenthost" || strings.HasPrefix(imported, modulePath+"/componenthost/registry") || strings.HasPrefix(imported, modulePath+"/componenthost/state") || strings.HasPrefix(imported, modulePath+"/componenthost/installstate") || strings.HasPrefix(imported, modulePath+"/componenthost/enabledstate") {
				t.Errorf("%s directly selects Component owners through %s", relative, imported)
			}
		}
	}
}
