package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectionPolicyCannotAcquireConcreteBackendOrNetworkExecution(t *testing.T) {
	root := moduleRoot(t)
	feature := modulePath + "/components/server-protection/"
	allowed := map[string]bool{
		modulePath + "/componenthost/hostref":         true,
		modulePath + "/componenthost/resources":       true,
		modulePath + "/componenthost/hostsurface":     true,
		modulePath + "/componenthost/fallbacktargets": true,
		modulePath + "/network/intent":                true,
		feature + "domain":                            true,
		feature + "service/scoring":                   true,
		feature + "service/policy":                    true,
		feature + "service/resources":                 true,
	}
	for _, relative := range []string{
		"components/server-protection/domain", "components/server-protection/service/decision",
		"components/server-protection/service/scoring", "components/server-protection/service/policy",
		"components/server-protection/service/response", "components/server-protection/service/enforcement",
		"components/server-protection/service/resources", "network/intent",
	} {
		walkGoFiles(t, filepath.Join(root, filepath.FromSlash(relative)), func(path string) {
			if strings.HasSuffix(path, "_test.go") {
				return
			}
			for _, imported := range fileImports(t, path) {
				if strings.HasPrefix(imported, modulePath+"/") && !allowed[imported] ||
					imported == "os" || imported == "os/exec" || imported == "database/sql" ||
					strings.HasPrefix(imported, "gorm.io/") || strings.HasPrefix(imported, "github.com/gin-gonic/") {
					t.Errorf("portable policy acquired concrete authority: %s imports %s", path, imported)
				}
			}
		})
	}
}

func TestTypedNetworkVocabularyCannotBecomeAnotherPlannerOrExecutor(t *testing.T) {
	walkGoFiles(t, filepath.Join(moduleRoot(t), "network", "intent"), func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			block, ok := declaration.(*ast.GenDecl)
			if !ok || block.Tok != token.IMPORT && block.Tok != token.TYPE && block.Tok != token.CONST {
				t.Fatalf("typed network vocabulary gained behavior or mutable state: %s", path)
			}
		}
	})
}
