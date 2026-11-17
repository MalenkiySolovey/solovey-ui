package architecture

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const backendModulePath = "github.com/MalenkiySolovey/solovey-backend"

func backendSourcePath(t *testing.T, relative string) string {
	t.Helper()
	command := exec.Command("go", "list", "-mod=readonly", "-m", "-json", backendModulePath)
	command.Dir = moduleRoot(t)
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	data, err := command.Output()
	if err != nil {
		t.Fatalf("resolve selected Backend source: %v", err)
	}
	var selected struct {
		Path, Version, Dir, Sum string
		Main                    bool
		Replace                 *json.RawMessage
	}
	if err := json.Unmarshal(data, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.Path != backendModulePath || selected.Version == "" || selected.Dir == "" || selected.Sum == "" || selected.Main || selected.Replace != nil {
		t.Fatalf("Backend source is not an immutable downloaded module: %+v", selected)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		t.Fatal("invalid Backend source path")
	}
	return filepath.Join(selected.Dir, clean)
}

func canonicalSourcePath(t *testing.T, relative string) string {
	t.Helper()
	if strings.HasPrefix(relative, "backend/") {
		return backendSourcePath(t, strings.TrimPrefix(relative, "backend/"))
	}
	return filepath.Join(moduleRoot(t), filepath.FromSlash(relative))
}

func TestProductRetiresEditableBackendAndUsesIndependentModule(t *testing.T) {
	root := moduleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "backend")); !os.IsNotExist(err) {
		t.Fatalf("editable Backend source remains: %v", err)
	}
	backendSourcePath(t, "go.mod")
	walkProductGoFiles(t, root, func(path string) {
		for _, imported := range fileImports(t, path) {
			if imported == modulePath+"/backend" || strings.HasPrefix(imported, modulePath+"/backend/") {
				t.Errorf("retired Backend import in %s: %s", path, imported)
			}
		}
	})
}
