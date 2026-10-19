package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
