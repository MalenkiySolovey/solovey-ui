package architecture

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInboundPersistenceConsumesOnlyItsRuntimeOperationSet(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{"service/netentity/inbounds.go", "internal/entities/inbounds/sessionidentity/capture.go"} {
		for _, imported := range fileImports(t, filepath.Join(root, filepath.FromSlash(relative))) {
			if imported == modulePath+"/core/runtime" || imported == modulePath+"/core/inboundidentity" {
				t.Errorf("%s imports concrete runtime owner %s", relative, imported)
			}
		}
	}
	for name := range goListDependencies(t, root, "./internal/entities/inbounds/sessionidentity") {
		for _, forbidden := range []string{"core/runtime", "core/inboundidentity", "core/box"} {
			if name == modulePath+"/"+forbidden || strings.HasPrefix(name, modulePath+"/"+forbidden+"/") {
				t.Errorf("credential binding captures runtime implementation %s", name)
			}
		}
	}
}
