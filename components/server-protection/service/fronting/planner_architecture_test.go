package fronting

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadOnlyPlannerHasNoMutationPersistenceOrPublicTransportDependency(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("planner source location unavailable")
	}
	root := filepath.Dir(current)
	files := []string{"runtime_identity_v2.go", "planner_contracts_v2.go", "planner_v2.go"}
	forbidden := []string{
		"components/server-protection/service/helper", "components/server-protection/service/operations",
		"components/server-protection/service/artifacts", "components/server-protection/service/repository",
		"gorm.io/", "database/", "net/http", "net.Dial", "os.WriteFile",
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, marker := range forbidden {
			if strings.Contains(text, marker) {
				t.Errorf("%s contains forbidden read-only dependency %q", name, marker)
			}
		}
	}
}

func TestReadOnlyPlannerDefinesNoPackageMutableState(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("planner source location unavailable")
	}
	root := filepath.Dir(current)
	for _, name := range []string{"runtime_identity_v2.go", "planner_contracts_v2.go", "planner_v2.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if ok && general.Tok == token.VAR {
				t.Errorf("%s defines package-level mutable state", name)
			}
		}
	}
}

func TestEndpointLeaseContractDefinesNoAuthorityMutationMethods(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	path := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", "..", "componenthost", "resources", "fronting_contracts.go"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, signature := range []string{"func (l EndpointLeaseV1) Acquire", "func (l EndpointLeaseV1) Renew", "func (l EndpointLeaseV1) Activate", "func (l EndpointLeaseV1) Release"} {
		if strings.Contains(text, signature) {
			t.Errorf("neutral lease contract gained provider mutation method %q", signature)
		}
	}
}
