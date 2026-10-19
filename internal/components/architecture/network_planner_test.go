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
