package coreinboundcontrol

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCapabilityResolverRevisionMatchesSource(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller location unavailable")
	}
	hash := sha256.New()
	for _, name := range []string{"capability.go", "identity.go", "provider_identity.go"} {
		content, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), name))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(hash, "%s\x00", name)
		hash.Write([]byte(strings.ReplaceAll(string(content), "\r\n", "\n")))
	}
	revision := fmt.Sprintf("%x", hash.Sum(nil))
	if revision != CapabilityResolverRevisionV1 {
		t.Fatalf("capability resolver revision = %s, want %s", CapabilityResolverRevisionV1, revision)
	}
}

func TestModuleFilesAndShippedProfilesMatchPinnedIdentity(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller location unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	assertContains := func(path string, values ...string) {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if !strings.Contains(string(content), value) {
				t.Fatalf("%s does not contain pinned value %q", path, value)
			}
		}
	}
	assertContains("go.mod", PinnedSingBoxModule+" "+PinnedSingBoxVersion, PinnedUTLSModule+" "+PinnedUTLSVersion)
	assertContains("go.sum", PinnedSingBoxModule+" "+PinnedSingBoxVersion+" "+PinnedSingBoxModuleSum,
		PinnedUTLSModule+" "+PinnedUTLSVersion+" "+PinnedUTLSModuleSum)
	assertContains(filepath.Join("windows", "build-windows.bat"), "build-windows.ps1", "%*")
	for _, path := range []string{"build.sh", "Dockerfile",
		filepath.Join("windows", "build-windows.ps1"), filepath.Join("scripts", "dev", "start-panel.ps1")} {
		assertContains(path, "with_utls")
	}
}
