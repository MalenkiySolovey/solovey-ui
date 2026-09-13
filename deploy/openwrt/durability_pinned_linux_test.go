//go:build linux

package openwrt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Real Linux mount namespace, using the source-verified fstools pivot shape.
// ext4 is host evidence; the exact physical F2FS tuple has separate source tests.
func TestPinnedFSToolsHostMountAuthority(t *testing.T) {
	if os.Getenv("SUI_REAL_OVERLAY_TESTS") != "1" {
		t.Skip("explicit root Linux mount tests")
	}
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("unshare", "--mount", "--propagation", "private", executable, "-test.run=^TestPinnedFSToolsHostMountAuthorityChild$", "-test.v")
	command.Env = append(os.Environ(), "SUI_PINNED_MOUNT_ROOT="+root)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("real pinned shape: %v\n%s", err, out)
	}
}

func TestPinnedFSToolsHostMountAuthorityChild(t *testing.T) {
	root := os.Getenv("SUI_PINNED_MOUNT_ROOT")
	if root == "" {
		t.Skip("namespace child")
	}
	for _, name := range []string{"lower", "overlay", "merged", "proc"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Create(filepath.Join(root, "backing.ext4"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	runFixtureCommand(t, "mkfs.ext4", "-q", "-F", filepath.Join(root, "backing.ext4"))
	runFixtureCommand(t, "mount", "-t", "ext4", "-o", "loop,nosuid,nodev", filepath.Join(root, "backing.ext4"), filepath.Join(root, "overlay"))
	if err := unix.Mount("/proc", filepath.Join(root, "proc"), "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chroot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"/overlay/upper", "/overlay/work", "/lower/overlay", "/lower/proc", "/lower/etc/solovey-ui/db"} {
		if err := os.MkdirAll(name, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount("overlayfs:/overlay", "/merged", "overlay", 0, "lowerdir=/lower,upperdir=/overlay/upper,workdir=/overlay/work"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("/overlay", "/merged/overlay", "", unix.MS_MOVE, ""); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("/proc", "/merged/proc", "", unix.MS_MOVE, ""); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chroot("/merged"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	proof, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, productionDurabilityEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if proof.ProofClass != PinnedFSToolsOverlay || proof.Overlay.BackingMount.Filesystem != "ext4" {
		t.Fatalf("proof=%#v", proof)
	}
	if _, err := recheckDatabaseDurabilityProof(proof, 4096, productionDurabilityEnvironment(), func(string) (uint64, error) { return 8192, nil }); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(DefaultDatabaseFolder)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("read-only persistence admission created writeback evidence")
	}
	// Actual producer/publication/reader contract on the mounted host fixture.
	written, err := RefreshDatabaseDurabilityProof()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDatabaseDurabilityProof()
	if err != nil || loaded.Revision != written.Revision {
		t.Fatalf("published proof: %v", err)
	}
	if err := os.WriteFile(DefaultDurabilityProofPath, []byte(`{"schema":"solovey-ui/openwrt-database-durability/v2"}`), 0444); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDatabaseDurabilityProof(); err == nil {
		t.Fatal("legacy proof accepted")
	}
	if _, err := RefreshDatabaseDurabilityProof(); err != nil {
		t.Fatalf("legacy proof was not replaced: %v", err)
	}
}
