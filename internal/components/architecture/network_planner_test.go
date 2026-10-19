package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComponentNetworkAdapterConstructionIsBrokerScoped(t *testing.T) {
	for _, fixture := range []struct {
		path, alias, expression string
		blocked                 bool
	}{
		{"components/example/feature.go", "n", "n.NewEngine", true},
		{"components/example/feature.go", "alias", "alias.NewEngineWithExecutor", true},
		{"components/example/feature.go", ".", "NewEngine", true},
		{"components/server-protection/service/helper/engine.go", "n", "n.NewEngine", false},
		{"components/server-protection/service/firewall/preview.go", "n", "n.RenderProtection", false},
	} {
		source := "package fixture\nimport " + fixture.alias + " \"" + modulePath + "/network/nftables\"\nvar selected = " + fixture.expression
		file, err := parser.ParseFile(token.NewFileSet(), fixture.path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, forbidden := inspectFoundationFile(fixture.path, file)
		if (len(forbidden) != 0) != fixture.blocked {
			t.Fatalf("%s %s: forbidden=%v", fixture.path, fixture.expression, forbidden)
		}
	}
}

func TestRetiredComponentNetworkAdapterStaysAbsent(t *testing.T) {
	root := moduleRoot(t)
	for _, name := range []string{"nft_backend.go", "nft_semantics.go", "nft_rollback.go"} {
		path := filepath.Join(root, "components", "server-protection", "service", "helper", name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("retired Component network implementation returned: %s (%v)", name, err)
		}
	}
}

// The migrated writer budget is zero. Allocation names and nft source must
// remain in the Core Mechanism; Components submit typed intent to its adapter.
func TestComponentsDoNotAllocateOrRenderHostNetworkResources(t *testing.T) {
	walkGoFiles(t, filepath.Join(moduleRoot(t), "components"), func(path string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{"solovey_protection", "solovey_endpoint_", "table inet ", "delete table ", "flush ruleset"} {
			if strings.Contains(string(data), token) {
				t.Errorf("Component host-network writer/allocation budget exceeded in %s: %q", path, token)
			}
		}
	})
}

func TestNetworkPlannerHasNoComponentOrPolicyDependency(t *testing.T) {
	root := moduleRoot(t)
	assertNoImports(t, root, []string{"network/planner", "network/nftables"}, []string{
		modulePath + "/components", modulePath + "/service", modulePath + "/database",
	}, "network mechanism ownership")
	for _, relative := range []string{"network/planner"} {
		walkGoFiles(t, filepath.Join(root, filepath.FromSlash(relative)), func(path string) {
			if strings.HasSuffix(path, "_test.go") {
				return
			}
			for _, imported := range fileImports(t, path) {
				if imported == "os/exec" || imported == "os" {
					t.Errorf("pure Planner acquired platform mutation authority in %s", path)
				}
			}
		})
	}
}
