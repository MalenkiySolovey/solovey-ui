package architecture

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const protectionSourceModule = "github.com/MalenkiySolovey/solovey-component-server-protection"

var protectionSourceOnce sync.Once
var protectionSourceDirectory string
var protectionSourceError error

// Inspect the same selected implementation that the actual Product imports.
// Immutable identity and checksums are independently enforced by source inputs.
func protectionSourcePath(t *testing.T, relative string) string {
	t.Helper()
	protectionSourceOnce.Do(func() {
		command := exec.Command("go", "list", "-m", "-json", protectionSourceModule)
		command.Dir = moduleRoot(t)
		var metadata struct{ Path, Dir string }
		data, err := command.Output()
		if err == nil {
			err = json.Unmarshal(data, &metadata)
		}
		protectionSourceError = err
		if metadata.Path == protectionSourceModule {
			protectionSourceDirectory = metadata.Dir
		}
	})
	if protectionSourceError != nil || !filepath.IsAbs(protectionSourceDirectory) {
		t.Fatalf("actual protection source unavailable: %v", protectionSourceError)
	}
	value := filepath.Join(protectionSourceDirectory, filepath.FromSlash(relative))
	within, err := filepath.Rel(protectionSourceDirectory, value)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		t.Fatal("protection source path escaped selected module")
	}
	if _, err := os.Stat(value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProtectionFeatureHasOneEditableSource(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{"domain", "service/decision", "service/scoring", "service/policy", "service/response", "service/enforcement", "service/resources", "service/events", "service/observationio", "service/observation", "service/durable", "service/operations", "service/helpercontract", "service/health", "service/graylist", "service/interception", "service/localproxy", "service/artifactcontract", "service/firewall", "service/udpguard", "service/nativefallback", "service/fronting"} {
		directory := filepath.Join(root, "components/server-protection", filepath.FromSlash(relative))
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		walkGoFiles(t, directory, func(name string) { t.Errorf("retired editable protection implementation returned: %s", name) })
	}
	frontend := filepath.Join(root, "components/server-protection/frontend")
	if err := filepath.WalkDir(frontend, func(name string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			t.Errorf("retired editable protection UI source returned: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
