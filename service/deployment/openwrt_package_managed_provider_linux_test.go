//go:build linux

package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

const packageProofUserNamespace = "SUI_TEST_PACKAGE_PROOF_USER_NAMESPACE"

func TestOpenWrtPackageManagedProviderLoadsRealExclusiveFacts(t *testing.T) {
	root := os.Getenv(packageProofUserNamespace)
	if root == "" {
		reexecPackageProofTest(t)
		return
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/solovey-ui"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "etc/solovey-ui"), 0o750); err != nil {
		t.Fatal(err)
	}
	// Restore the subprocess root after the real fixture so Go's coverage
	// runtime can flush its counters to the original output directory.
	originalRoot, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	defer originalRoot.Close()
	defer func() {
		if err := originalRoot.Chdir(); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Chroot("."); err != nil {
			t.Fatal(err)
		}
	}()
	if err := syscall.Chroot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range []struct {
		name        string
		openWrt     bool
		systemd     bool
		wantSuccess bool
	}{
		{name: "valid-openwrt", openWrt: true, wantSuccess: true},
		{name: "systemd-only", systemd: true},
		{name: "missing"},
		{name: "ambiguous", openWrt: true, systemd: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			systemdPath := deploymentidentity.InstalledContractPath
			openWrtPath := deploymentidentity.ProcdInstalledContractPath
			for _, installedPath := range []string{systemdPath, openWrtPath} {
				if err := os.Remove(installedPath); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if fixture.openWrt {
				writeRootOwnedJSON(t, openWrtPath, openWrtOwnerProof(t))
			}
			if fixture.systemd {
				writeRootOwnedJSON(t, systemdPath, systemdOwnerProof(t))
			}

			t.Setenv("SUI_DEPLOYMENT_KIND", OpenWrtPackageManagedDeploymentKind)
			provider, ok := RuntimeProvider().(*OpenWrtPackageManagedProvider)
			if !ok {
				t.Fatalf("explicit package-managed provider selected %T", RuntimeProvider())
			}
			posture, err := provider.Observe(context.Background())
			if fixture.wantSuccess {
				if err != nil || posture.Profile != domain.PackageManagedOpenWrt || posture.Runtime != domain.RuntimePackageManaged {
					t.Fatalf("real OpenWrt proof posture = %#v, %v", posture, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s real fact set projected an OpenWrt posture", fixture.name)
			}
		})
	}
}

func reexecPackageProofTest(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	arguments := []string{os.Args[0], "-test.run=^TestOpenWrtPackageManagedProviderLoadsRealExclusiveFacts$", "-test.v"}
	var command *exec.Cmd
	if os.Geteuid() == 0 {
		command = exec.Command(arguments[0], arguments[1:]...)
	} else {
		command = exec.Command("unshare", append([]string{"-Ur"}, arguments...)...)
	}
	command.Env = append(os.Environ(), packageProofUserNamespace+"="+root)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("root-owned provider composition subprocess: %v\n%s", err, output)
	}
}

func writeRootOwnedJSON(t testing.TB, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(data, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o444); err != nil {
		t.Fatal(err)
	}
}

func systemdOwnerProof(t testing.TB) deploymentidentity.ApplicationOwnerContractV1 {
	t.Helper()
	proof, err := deploymentidentity.NewSystemdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64),
		"art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64), strings.Repeat("6", 64), strings.Repeat("4", 64),
		"solovey-ui-panel", "solovey-ui.service", "/etc/systemd/system/solovey-ui.service", strings.Repeat("7", 64),
		"/system.slice/solovey-ui.service", "/usr/lib/solovey-ui/solovey-ui", strings.Repeat("5", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(fmt.Errorf("create Systemd owner proof: %w", err))
	}
	return proof
}
