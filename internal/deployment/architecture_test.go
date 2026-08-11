package deployment

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const deploymentModule = "github.com/MalenkiySolovey/solovey-ui"

func TestDeploymentArchitectureKeepsDomainBrokerAndLabBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	domainRoot := filepath.Join(root, "internal", "deployment")
	serviceRoot := filepath.Join(root, "service", "deployment")
	brokerRoot := filepath.Join(root, "internal", "ops", "deploymentbroker")

	forEachProductionGoFile(t, domainRoot, func(path string, imports []string, source string) {
		for _, imported := range imports {
			if strings.HasPrefix(imported, deploymentModule+"/") {
				t.Fatalf("deployment domain imports product implementation: %s imports %s", path, imported)
			}
		}
		assertNoDeploymentTestDoubleOrLabDependency(t, path, source)
	})
	forEachProductionGoFile(t, serviceRoot, func(path string, imports []string, source string) {
		for _, imported := range imports {
			if strings.HasPrefix(imported, deploymentModule+"/components/server-protection") {
				t.Fatalf("deployment service delegates ownership to server-protection: %s imports %s", path, imported)
			}
		}
		assertNoDeploymentTestDoubleOrLabDependency(t, path, source)
	})
	forEachProductionGoFile(t, brokerRoot, func(path string, imports []string, source string) {
		for _, imported := range imports {
			if !strings.HasPrefix(imported, deploymentModule+"/") {
				continue
			}
			if imported != deploymentModule+"/internal/deployment" && imported != deploymentModule+"/internal/ops/privilegedbroker" {
				t.Fatalf("deployment broker crosses its semantic boundary: %s imports %s", path, imported)
			}
		}
		assertNoDeploymentTestDoubleOrLabDependency(t, path, source)
	})
}

func assertNoDeploymentTestDoubleOrLabDependency(t *testing.T, path, source string) {
	t.Helper()
	for _, forbidden := range []string{"fakeProvider", "FakeProvider", "tools/lab.cmd", "qemu-guest-agent"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("production deployment source %s contains forbidden dependency %q", path, forbidden)
		}
	}
}

func forEachProductionGoFile(t *testing.T, root string, visit func(path string, imports []string, source string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
		if err != nil {
			return err
		}
		imports := make([]string, 0, len(parsed.Imports))
		for _, spec := range parsed.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports = append(imports, value)
		}
		visit(path, imports, string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
