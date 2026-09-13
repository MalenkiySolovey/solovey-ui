package architecture

import (
	"path/filepath"
	"strings"
	"testing"
)

// Root process execution is permitted only inside the packages that own a
// released privileged semantic operation. The assertion is package/import
// based, so moving or splitting a source file cannot make it pass or fail.
// Real object trust and execution-time continuity are covered by each owner's
// Linux filesystem behavior tests.
func TestPrivilegedExecutionPackagesConsumeTrustedObjectMechanisms(t *testing.T) {
	root := moduleRoot(t)
	allowedOwners := map[string]bool{
		"cmd/solovey-openwrt-lifecycle":               true,
		"components/server-protection/service/helper": true,
		"internal/ops/deploymentbroker":               true,
		"internal/ops/privilegedbroker":               true,
		"internal/ops/sshbroker":                      true,
		"internal/ops/updatebroker":                   true,
	}
	mechanisms := map[string]bool{
		modulePath + "/internal/ops/executableobject": true,
		modulePath + "/internal/ops/procdexec":        true,
		modulePath + "/internal/ops/systemdexec":      true,
	}
	type packageFacts struct {
		execFiles []string
		imports   map[string]bool
	}
	packages := map[string]*packageFacts{}
	walkProductGoFiles(t, root, func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		packagePath := filepath.ToSlash(filepath.Dir(rel))
		facts := packages[packagePath]
		if facts == nil {
			facts = &packageFacts{imports: map[string]bool{}}
			packages[packagePath] = facts
		}
		for _, imported := range fileImports(t, path) {
			facts.imports[imported] = true
			if imported == "os/exec" {
				facts.execFiles = append(facts.execFiles, rel)
			}
		}
	})
	for packagePath, facts := range packages {
		if len(facts.execFiles) == 0 {
			continue
		}
		if !allowedOwners[packagePath] {
			t.Errorf("package %s gained process execution outside a privileged semantic owner: %v", packagePath, facts.execFiles)
			continue
		}
		usesMechanism := false
		for imported := range facts.imports {
			usesMechanism = usesMechanism || mechanisms[imported]
		}
		if !usesMechanism {
			t.Errorf("privileged execution package %s does not consume a trusted executable-object mechanism", packagePath)
		}
	}
}
