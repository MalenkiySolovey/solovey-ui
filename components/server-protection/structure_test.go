package serverprotection

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var phaseNamePattern = regexp.MustCompile(`(?i)phase[-_]?[0-9]+`)

func TestComponentStructureUsesResponsibilityNames(t *testing.T) {
	root := "."
	forbiddenPackages := map[string]struct{}{
		"util": {}, "utils": {}, "common": {}, "misc": {}, "helpers": {},
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		cleanPath := filepath.ToSlash(path)
		if entry.IsDir() {
			if phaseNamePattern.MatchString(name) {
				t.Errorf("phase-numbered directory is forbidden: %s", cleanPath)
			}
			if _, forbidden := forbiddenPackages[strings.ToLower(name)]; forbidden {
				t.Errorf("generic package directory is forbidden: %s", cleanPath)
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(name), ".go") && phaseNamePattern.MatchString(name) {
			t.Errorf("phase-numbered Go source file is forbidden: %s", cleanPath)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSourceDoesNotContainOrImportNormalCITestSupport(t *testing.T) {
	const normalCIImport = "github.com/MalenkiySolovey/solovey-ui/components/server-protection/internal/normalci/"
	forbidden := []string{
		"type FakeNginxExecutor",
		"type FakeListenerExecutor",
		"type FakeProcessExecutor",
		"type MockInvoker",
		"type MockRecovery",
		"func NewMockInvoker",
		"func NewDefaultInvoker",
		"func NewFirewallMockInvoker",
		"func NewFrontingMockInvoker",
		"func NewFakeNginxExecutor",
		"func NewContractEngineWithExecutor",
		"func NewContractEngineWithExecutors",
		"func NewContractEngineWithBackends",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		cleanPath := filepath.ToSlash(path)
		if entry.IsDir() {
			if cleanPath == "internal/normalci" || strings.HasPrefix(cleanPath, "internal/normalci/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := fs.ReadFile(os.DirFS("."), cleanPath)
		if err != nil {
			return err
		}
		text := string(data)
		if strings.Contains(text, normalCIImport) {
			t.Errorf("runtime source imports normal-CI support: %s", cleanPath)
		}
		for _, marker := range forbidden {
			if strings.Contains(text, marker) {
				t.Errorf("runtime source retains test-only surface %q: %s", marker, cleanPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServicePackagesStayTransportIndependent(t *testing.T) {
	err := filepath.WalkDir("service", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "github.com/gin-gonic/gin") {
			t.Errorf("service package imports HTTP transport: %s", filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUDPHTTPTransportDoesNotOwnMutationAuthority(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("api", "udp_guard.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{
		".Repository.",
		".Firewall.Prepare",
		".Firewall.Apply",
		".Firewall.Rollback",
		"UDPGuardStates(",
		"SaveUDPGuardState(",
		"FirewallAuthority(",
		"DefaultProtocolProbesV1",
		"PostApplyHealth:",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("UDP HTTP transport owns service authority %q", forbidden)
		}
	}
}
