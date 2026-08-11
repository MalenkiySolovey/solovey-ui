package nativefallback

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestNativePlannerImportsOnlyReadOnlyNeutralBoundaries(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller location unavailable")
	}
	directory := filepath.Dir(currentFile)
	plannerFiles := []string{"planner.go", "target_reader.go", "management.go"}
	parsedFiles := make([]*ast.File, 0, len(plannerFiles))
	for _, name := range plannerFiles {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		parsedFiles = append(parsedFiles, parsed)
	}
	forbidden := []string{"components/server-protection/api", "components/server-protection/service/helper", "components/server-protection/service/operations"}
	for _, file := range parsedFiles {
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, marker := range forbidden {
				if strings.Contains(path, marker) {
					t.Fatalf("native planner imports forbidden boundary %q", path)
				}
			}
		}
	}
}

func TestPlannerInterfacesExposeNoMutationSurface(t *testing.T) {
	interfaces := []reflect.Type{
		reflect.TypeOf((*CoreReader)(nil)).Elem(),
		reflect.TypeOf((*TargetReader)(nil)).Elem(),
		reflect.TypeOf((*ManagementReader)(nil)).Elem(),
	}
	for _, contract := range interfaces {
		for index := 0; index < contract.NumMethod(); index++ {
			name := contract.Method(index).Name
			for _, forbidden := range []string{"Apply", "Prepare", "Checkpoint", "Restore", "Release", "Reserve", "Fence", "Activate", "Renew", "Save", "Write"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("read contract %s exposes mutation method %s", contract, name)
				}
			}
		}
	}
}
