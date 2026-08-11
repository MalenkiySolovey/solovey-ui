package sshmanagement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionServiceHasNoDirectHostMutationOrEmbeddedFake(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, forbidden := range []string{"os/exec", "exec.Command", "syscall.", "net.Dial", "/etc/ssh/", "workflowProviderFake"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s contains forbidden production capability %q", entry.Name(), forbidden)
			}
		}
	}
}

func TestServerProtectionCompositionDoesNotOwnSSHRecovery(t *testing.T) {
	path := filepath.Join("..", "..", "components", "server-protection", "component.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"SSHObserver", "PanelWriter", "RegisterEvidenceProvider", "RegisterPanel"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("server-protection composition retains SSH/recovery ownership %q", forbidden)
		}
	}
}
