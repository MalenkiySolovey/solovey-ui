package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendFrontendAssetRuntimeHasPortableClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{modulePath + "/backend/http/frontend": true}
	for name := range goListDependencies(t, root, "github.com/gin-gonic/gin") {
		allowed[name] = true
	}
	var rejected []string
	for name, info := range goListDependencies(t, root, "./backend/http/frontend") {
		if !info.Standard && !allowed[name] {
			rejected = append(rejected, name)
		}
	}
	if len(rejected) > 0 {
		sort.Strings(rejected)
		t.Fatalf("frontend asset runtime imports unrelated owner: %s", strings.Join(rejected, ", "))
	}
}
