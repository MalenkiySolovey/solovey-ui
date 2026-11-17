package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type goListPackage struct {
	ImportPath string
	Standard   bool
}

func TestBackendHostContextHasPortableTransitiveImportClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{
		modulePath + "/backend/hostcontext": true,
	}
	for _, pattern := range []string{
		"github.com/gin-gonic/gin",
		"github.com/MalenkiySolovey/solovey-host/hostref",
	} {
		for importPath := range goListDependencies(t, root, pattern) {
			allowed[importPath] = true
		}
	}

	targetDependencies := goListDependencies(t, root, "./backend/hostcontext")
	var violations []string
	for importPath, packageInfo := range targetDependencies {
		if packageInfo.Standard || allowed[importPath] {
			continue
		}
		violations = append(violations, importPath)
	}
	if len(violations) > 0 {
		t.Fatalf("backend/hostcontext has imports outside its portable closure: %s", strings.Join(violations, ", "))
	}

	var directViolations []string
	walkGoFiles(t, filepath.Join(root, "backend", "hostcontext"), func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		relative := filepath.ToSlash(mustRel(t, root, path))
		for _, imported := range fileImports(t, path) {
			packageInfo, known := targetDependencies[imported]
			if (known && packageInfo.Standard) || imported == "github.com/gin-gonic/gin" || imported == "github.com/MalenkiySolovey/solovey-host/hostref" {
				continue
			}
			directViolations = append(directViolations, relative+" imports "+imported)
		}
	})
	if len(directViolations) > 0 {
		t.Fatalf("backend/hostcontext has direct imports outside its owner boundary: %s", strings.Join(directViolations, ", "))
	}
}

func TestBackendHostContextIsTheAPIIngressOwner(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{"api/apiHandler.go", "api/apiV2Handler.go"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		imports := fileImports(t, path)
		if !containsString(imports, modulePath+"/backend/hostcontext") {
			t.Errorf("%s does not import backend/hostcontext", relative)
		}
		if containsString(imports, modulePath+"/middleware/hostcontext") {
			t.Errorf("%s still imports the retired middleware/hostcontext owner", relative)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash("middleware/hostcontext"))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("retired middleware/hostcontext package still exists: %v", err)
	}
}

func goListDependencies(t *testing.T, root, pattern string, options ...string) map[string]goListPackage {
	t.Helper()
	args := append([]string{"list", "-mod=readonly", "-deps", "-json"}, options...)
	args = append(args, pattern)
	command := exec.Command("go", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("go list dependency closure for %s: %v\n%s", pattern, err, strings.TrimSpace(stderr.String()))
	}

	packages := map[string]goListPackage{}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	for {
		var packageInfo goListPackage
		err := decoder.Decode(&packageInfo)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode go list dependency closure for %s: %v", pattern, err)
		}
		if packageInfo.ImportPath != "" {
			packages[packageInfo.ImportPath] = packageInfo
		}
	}
	return packages
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
