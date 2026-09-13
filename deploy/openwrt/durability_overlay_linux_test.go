//go:build linux

package openwrt

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
	"golang.org/x/sys/unix"
)

const realOverlayTestEnvironment = "SUI_REAL_OVERLAY_TESTS"
const realOverlayCaseEnvironment = "SUI_REAL_OVERLAY_CASE"

func TestRealOverlayDurabilityMatrix(t *testing.T) {
	if os.Getenv(realOverlayTestEnvironment) != "1" {
		t.Skip("set SUI_REAL_OVERLAY_TESTS=1 in an authorized root Linux test environment")
	}
	if os.Geteuid() != 0 {
		t.Fatal("real OverlayFS durability matrix requires root")
	}
	for _, command := range []string{"unshare", "mount", "mkfs.ext4"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Fatalf("required command %s is unavailable: %v", command, err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"persistent-visible", "persistent-hidden", "tmpfs-visible", "tmpfs-hidden",
		"read-only", "overlay-volatile", "malformed-options", "unknown-backing", "fsync-failure",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			command := exec.Command("unshare", "--mount", "--propagation", "private", executable,
				"-test.run=^TestRealOverlayDurabilityCase$", "-test.v", "-test.timeout=90s")
			command.Env = append(os.Environ(), realOverlayCaseEnvironment+"="+name)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("real overlay case %s failed: %v\n%s", name, err, output)
			}
		})
	}
}

func TestRealOverlayDurabilityCase(t *testing.T) {
	name := os.Getenv(realOverlayCaseEnvironment)
	if name == "" {
		t.Skip("inner mount-namespace helper")
	}
	runRealOverlayDurabilityCase(t, name)
}

func runRealOverlayDurabilityCase(t *testing.T, name string) {
	root, err := os.MkdirTemp("/var/tmp", "solovey-real-overlay-")
	if err != nil {
		t.Fatal(err)
	}
	lower := filepath.Join(root, "lower")
	backing := filepath.Join(root, "backing")
	upper := filepath.Join(backing, "upper")
	work := filepath.Join(backing, "work")
	merged := filepath.Join(root, "merged")
	for _, directory := range []string{lower, backing, merged, filepath.Join(lower, "etc", "solovey-ui", "db")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mergedMounted, backingMounted := false, false
	defer func() {
		if mergedMounted {
			_ = unix.Unmount(merged, unix.MNT_DETACH)
		}
		if backingMounted {
			_ = unix.Unmount(backing, unix.MNT_DETACH)
		}
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove real overlay fixture: %v", err)
		}
	}()

	if name == "malformed-options" {
		err := unix.Mount("overlay", merged, "overlay", 0, "lowerdir="+lower+",upperdir=relative,workdir="+work)
		if err == nil {
			mergedMounted = true
			t.Fatal("kernel accepted malformed relative OverlayFS upperdir")
		}
		return
	}

	switch name {
	case "tmpfs-visible", "tmpfs-hidden":
		if err := unix.Mount("tmpfs", backing, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=32m,mode=0755"); err != nil {
			t.Fatal(err)
		}
		backingMounted = true
	default:
		image := filepath.Join(root, "backing.ext4")
		file, err := os.OpenFile(image, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(64 << 20); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		runFixtureCommand(t, "mkfs.ext4", "-q", "-F", image)
		runFixtureCommand(t, "mount", "-t", "ext4", "-o", "loop,nosuid,nodev", image, backing)
		backingMounted = true
	}

	options := "lowerdir=" + lower
	for _, directory := range []string{upper, work} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	options += ",upperdir=" + upper + ",workdir=" + work
	if name == "overlay-volatile" {
		options += ",volatile"
	}
	if err := unix.Mount("overlay", merged, "overlay", 0, options); err != nil {
		t.Fatalf("mount overlay (%s): %v", options, err)
	}
	mergedMounted = true
	if name == "read-only" {
		runFixtureCommand(t, "mount", "-o", "remount,ro", merged)
	}

	hidden := name == "persistent-hidden" || name == "tmpfs-hidden"
	if hidden {
		if err := unix.Unmount(backing, unix.MNT_DETACH); err != nil {
			t.Fatalf("detach backing mount: %v", err)
		}
		backingMounted = false
		for _, label := range []string{upper, work} {
			if _, err := os.Lstat(label); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("detached label %s remains visible: %v", label, err)
			}
		}
	}

	target := filepath.Join(merged, "etc", "solovey-ui", "db")
	primary, err := mountevidence.Observe(target)
	if err != nil {
		t.Fatal(err)
	}
	// This is a real generic Linux overlay, not a root pivot created by the
	// deployment's fstools. Even persistent ext4 or positive FIEMAP capability
	// must not authorize an unknown topology through a writeback probe.
	_, proofErr := observeFSToolsOverlay(primary, productionDurabilityEnvironment())
	if !errors.Is(proofErr, ErrUnprovenDurableState) {
		t.Fatalf("noncanonical overlay case %s was accepted: %v", name, proofErr)
	}
}

func runFixtureCommand(t *testing.T, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(arguments, " "), err, output)
	}
}
