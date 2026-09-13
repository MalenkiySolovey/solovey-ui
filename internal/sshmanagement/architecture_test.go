package sshmanagement

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestDesiredPolicyHasNoConcreteSSHArtifactDependency(t *testing.T) {
	source, err := os.ReadFile("types.go")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(gotoken.NewFileSet(), "types.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range parsed.Imports {
		if filepath.Base(imported.Path.Value) == "ssh" || imported.Path.Value == `"os/exec"` || imported.Path.Value == `"syscall"` {
			t.Fatalf("semantic SSH owner imports a host artifact authority: %s", imported.Path.Value)
		}
	}
	forbidden := map[string]bool{"RenderManagedDropIn": true, "openssh": true, "dropbear": true, "systemd": true, "procd": true, "journald": true, "logread": true}
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && forbidden[identifier.Name] {
			t.Errorf("semantic SSH owner contains concrete artifact identifier %q", identifier.Name)
		}
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "DesiredPolicyV1" {
			return true
		}
		structure, ok := spec.Type.(*ast.StructType)
		if !ok {
			t.Fatal("DesiredPolicyV1 is not a struct")
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				forbiddenField := map[string]bool{"Implementation": true, "Daemon": true, "Service": true, "Supervisor": true, "Transport": true, "LogEvidence": true, "Artifact": true, "DropIn": true, "UCI": true}
				if forbiddenField[name.Name] {
					t.Errorf("DesiredPolicyV1 exposes backend field %s", name.Name)
				}
			}
		}
		return false
	})
}
