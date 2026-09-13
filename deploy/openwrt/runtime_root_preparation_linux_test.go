//go:build linux

package openwrt

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"golang.org/x/sys/unix"
)

const openWrtRuntimePreparationChild = "SUI_OPENWRT_RUNTIME_PREPARATION_CHILD"

func TestOpenWrtPreparationPublishesContractsThroughCanonicalRuntimeRoot(t *testing.T) {
	if os.Getenv(openWrtRuntimePreparationChild) == "" {
		reexecOpenWrtRuntimePreparation(t)
		return
	}

	sourceRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binaries := buildRuntimePreparationBinaries(t, sourceRoot)
	pinned := pinnedOpenWrtSourceRoot(t)
	assertPinnedOpenWrtRevision(t, pinned)

	t.Run("volatile ancestor symlinks publish every runtime authority", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		installRuntimePreparationFixture(t, fixture, binaries)
		bindPreparationProc(t, fixture)
		t.Cleanup(func() {
			if err := unix.Unmount(fixture.hostPath("/tmp"), unix.MNT_DETACH); err != nil {
				t.Errorf("unmount fixture tmpfs: %v", err)
			}
		})
		fixture.run(t, strings.Join([]string{
			"mount -t tmpfs -o mode=1777,size=16m tmpfs /tmp",
			"mkdir -p /tmp/run",
			"/usr/lib/solovey-ui/solovey-openwrt-prepare",
		}, "\n"), nil, true)

		for _, name := range []string{
			deploymentidentity.ProcdInstalledContractPath,
			"/etc/solovey-ui/server-protection-runtime-root.json",
			"/tmp/run/solovey-ui/broker-clients.json",
		} {
			if info, err := os.Stat(fixture.hostPath(name)); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("preparation output %s = %v, %v", name, info, err)
			}
		}
		diagnostics, err := os.Lstat(fixture.hostPath("/tmp/run/solovey-ui/diagnostics"))
		if err != nil {
			t.Fatalf("diagnostic runtime root: %v", err)
		}
		stat, ownerOK := diagnostics.Sys().(*syscall.Stat_t)
		if !diagnostics.IsDir() || diagnostics.Mode().Perm() != 0o700 || diagnostics.Mode()&os.ModeSymlink != 0 || !ownerOK || stat.Uid != 0 || stat.Gid != 0 {
			t.Fatalf("diagnostic runtime root = %v", diagnostics)
		}
		data, err := os.ReadFile(fixture.hostPath("/etc/solovey-ui/server-protection-runtime-root.json"))
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			RuntimeRoot string `json:"runtimeRoot"`
			MountProof  struct {
				Policy string `json:"policy"`
				Root   string `json:"root"`
				Mount  struct {
					Target         string `json:"target"`
					ResolvedTarget string `json:"resolvedTarget"`
					Filesystem     string `json:"filesystem"`
				} `json:"mount"`
			} `json:"mountProof"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if document.RuntimeRoot != "/run/solovey-ui/server-protection" ||
			document.MountProof.Policy != "volatile" || document.MountProof.Root != document.RuntimeRoot ||
			document.MountProof.Mount.Target != document.RuntimeRoot ||
			document.MountProof.Mount.ResolvedTarget != "/tmp/run/solovey-ui/server-protection" ||
			document.MountProof.Mount.Filesystem != "tmpfs" {
			t.Fatalf("published logical/canonical runtime authority = %#v", document)
		}
	})

	t.Run("persistent runtime redirect fails before runtime and broker publication", func(t *testing.T) {
		fixture := newPinnedLifecycleRoot(t, pinned)
		installRuntimePreparationFixture(t, fixture, binaries)
		bindPreparationProc(t, fixture)
		fixture.run(t, "/usr/lib/solovey-ui/solovey-openwrt-prepare", nil, false)
		for _, name := range []string{
			"/etc/solovey-ui/server-protection-runtime-root.json",
			"/tmp/run/solovey-ui/broker-clients.json",
		} {
			if _, err := os.Lstat(fixture.hostPath(name)); !os.IsNotExist(err) {
				t.Fatalf("failed preparation published %s: %v", name, err)
			}
		}
	})
}

func reexecOpenWrtRuntimePreparation(t testing.TB) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root is required for the OpenWrt service-ownership preparation fixture")
	}
	arguments := []string{os.Args[0], "-test.run=^TestOpenWrtPreparationPublishesContractsThroughCanonicalRuntimeRoot$", "-test.v"}
	command := exec.Command("unshare", append([]string{"-m", "--"}, arguments...)...)
	command.Env = append(os.Environ(), openWrtRuntimePreparationChild+"=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("OpenWrt runtime preparation subprocess: %v\n%s", err, output)
	}
}

func buildRuntimePreparationBinaries(t testing.TB, sourceRoot string) map[string]string {
	t.Helper()
	outputRoot := t.TempDir()
	cacheRoot := filepath.Join(outputRoot, "go-cache")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	packages := map[string]string{
		"solovey-openwrt-owner-manifest":  "./components/server-protection/cmd/solovey-openwrt-owner-manifest",
		"solovey-openwrt-broker-manifest": "./cmd/solovey-openwrt-broker-manifest",
	}
	result := make(map[string]string, len(packages))
	for name, pkg := range packages {
		output := filepath.Join(outputRoot, name)
		command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-tags", "osusergo,netgo", "-ldflags", "-linkmode external -extldflags -static", "-o", output, pkg)
		command.Dir = sourceRoot
		command.Env = append(os.Environ(), "CGO_ENABLED=1", "GOOS=linux", "GOARCH="+runtime.GOARCH, "GOCACHE="+cacheRoot)
		if buildOutput, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, buildOutput)
		}
		result[name] = output
	}
	return result
}

func installRuntimePreparationFixture(t testing.TB, fixture *pinnedLifecycleRoot, binaries map[string]string) {
	t.Helper()
	prepare, err := os.ReadFile("solovey-openwrt-prepare")
	if err != nil {
		t.Fatal(err)
	}
	fixture.writeMode(t, "/usr/lib/solovey-ui/solovey-openwrt-prepare", prepare, 0o755)
	fixture.write(t, "/usr/lib/solovey-ui/solovey-openwrt-durability", "#!/bin/sh\nexit 0\n")
	fixture.writeMode(t, "/etc/passwd", []byte("root:x:0:0:root:/root:/bin/ash\nsolovey-ui:x:32768:32768:Solovey UI:/etc/solovey-ui:/sbin/nologin\n"), 0o644)
	fixture.writeMode(t, "/etc/group", []byte("root:x:0:\nsolovey-ui:x:32768:\n"), 0o644)
	fixture.writeMode(t, "/etc/nsswitch.conf", []byte("passwd: files\ngroup: files\n"), 0o644)
	fixture.writeMode(t, "/usr/lib/solovey-ui/BUILD_INFO.txt", []byte("commit=339a15051c95cea12e97dcdf939ab1fc6b941062\n"), 0o644)
	for _, name := range []string{"solovey-openwrt-owner-manifest", "solovey-openwrt-broker-manifest"} {
		data, err := os.ReadFile(binaries[name])
		if err != nil {
			t.Fatal(err)
		}
		fixture.writeMode(t, "/usr/lib/solovey-ui/"+name, data, 0o555)
	}
	for _, name := range []string{PanelExecutablePath, ReadinessExecutablePath, SSHProofExecutablePath, DropbearExecutablePath} {
		fixture.writeMode(t, name, []byte("fixture executable\n"), 0o555)
	}
	if err := os.MkdirAll(fixture.hostPath("/proc"), 0o555); err != nil {
		t.Fatal(err)
	}
}

func bindPreparationProc(t testing.TB, fixture *pinnedLifecycleRoot) {
	t.Helper()
	target := fixture.hostPath("/proc")
	if err := unix.Mount("/proc", target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(target, unix.MNT_DETACH); err != nil {
			t.Errorf("unmount fixture proc: %v", err)
		}
	})
}
