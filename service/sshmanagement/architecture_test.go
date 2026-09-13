package sshmanagement

import (
	"go/parser"
	gotoken "go/token"
	"os"
	"strings"
	"testing"
)

func TestProductionServiceImportsNoDirectHostMutationCapability(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := entry.Name()
		parsed, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			path := imported.Path.Value
			if path == `"os/exec"` || path == `"syscall"` || path == `"net"` {
				t.Fatalf("%s imports forbidden production capability %s", entry.Name(), path)
			}
		}
	}
}
