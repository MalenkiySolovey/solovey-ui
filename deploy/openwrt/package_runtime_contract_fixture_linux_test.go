//go:build linux

package openwrt_test

import (
	"os"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/testsupport/openwrtpackage"
)

// These helpers run as subprocesses of the full package-hook fixture. The
// cross-component composition lives under testsupport so deploy/openwrt keeps
// its production component-import boundary intact.
func TestPackageOwnerRuntimeWriterHelper(t *testing.T) {
	parent := os.Getenv("SUI_MODEL_PARENT")
	if parent == "" {
		t.Skip("package fixture helper only")
	}
	if err := openwrtpackage.WriteOwnerRuntime(parent); err != nil {
		t.Fatal(err)
	}
	appendPackageFixtureEvent("owner")
	appendPackageFixtureEvent("runtime-root-contract")
}

func TestPackageRuntimeContractReaderHelper(t *testing.T) {
	parent := os.Getenv("SUI_MODEL_PARENT")
	if parent == "" {
		t.Skip("package fixture helper only")
	}
	if err := openwrtpackage.ValidateOwnerRuntime(parent); err != nil {
		t.Fatal(err)
	}
	appendPackageFixtureEvent("runtime-root-validated")
}

func appendPackageFixtureEvent(event string) {
	path := os.Getenv("SUI_MODEL_EVENTS")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(event + "\n")
}
