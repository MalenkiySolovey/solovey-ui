package openwrt

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const pinnedFSToolsCommit = "16718b6e3c0fc7db7be6ae5848db0eae88ac8a8b"

func TestPersistenceAuthorityProjectionMatchesCanonicalLock(t *testing.T) {
	workspace := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	lock := pinnedSource(t, workspace, "upstreams/OPENWRT_REFERENCE_LOCK.md")
	field := func(section, expression string) string {
		t.Helper()
		parts := strings.SplitN(lock, "#### "+section+". ", 2)
		if len(parts) != 2 {
			t.Fatalf("canonical reference %s missing", section)
		}
		body := strings.SplitN(parts[1], "\n#### ", 2)[0]
		match := regexp.MustCompile(expression).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatalf("canonical reference %s field missing", section)
		}
		return match[1]
	}
	authority := selectedPersistenceAuthority()
	want := PersistenceAuthority{
		OpenWrtRelease: strings.TrimPrefix(field("A1", "\\*\\*Requested Ref\\*\\*: `([^`]+)`"), "v"),
		OpenWrtSource:  field("A1", "\\*\\*HEAD SHA\\*\\*: `([0-9a-f]{40})`"),
		FSToolsSource:  field("A2", "\\*\\*HEAD SHA\\*\\*: `([0-9a-f]{40})`"),
	}
	if authority != want {
		t.Fatalf("generated authority drifted: got %#v want %#v", authority, want)
	}
	platform := field("A1", "\\*\\*Directory\\*\\*: `([^`]+)`")
	fstools := field("A2", "\\*\\*Directory\\*\\*: `([^`]+)`")
	requireGitRevision(t, filepath.Join(workspace, platform), authority.OpenWrtSource)
	requireGitRevision(t, filepath.Join(workspace, fstools), authority.FSToolsSource)
	recipe := pinnedSource(t, filepath.Join(workspace, platform), "package/system/fstools/Makefile")
	if !strings.Contains(recipe, "PKG_SOURCE_VERSION:="+authority.FSToolsSource) {
		t.Fatal("canonical recipe/projection mismatch")
	}
	profiles := pinnedSource(t, filepath.Join("..", ".."), "scripts/openwrt-target-profile.sh")
	for _, match := range regexp.MustCompile("OPENWRT_SOURCE_COMMIT='([^']+)'").FindAllStringSubmatch(profiles, -1) {
		if match[1] != authority.OpenWrtSource {
			t.Fatal("release authority/projection mismatch")
		}
	}
}

func TestPinnedFSToolsPersistentAndRAMOverlaySemantics(t *testing.T) {
	workspaceRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	fstoolsRoot := filepath.Join(workspaceRoot, "upstreams", "openwrt-fstools-25.12.5-pinned")
	openwrtRoot := filepath.Join(workspaceRoot, "upstreams", "openwrt-openwrt-25.12.5")
	for _, root := range []string{fstoolsRoot, openwrtRoot} {
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("authoritative OpenWrt reference is unavailable at %s: %v", root, err)
		}
	}
	requireGitRevision(t, fstoolsRoot, pinnedFSToolsCommit)
	requireGitRevision(t, openwrtRoot, pinnedOpenWrtCommit)

	recipe := pinnedSource(t, openwrtRoot, "package/system/fstools/Makefile")
	for _, exact := range []string{
		"PKG_SOURCE_VERSION:=" + pinnedFSToolsCommit,
		"PKG_SOURCE_DATE:=2026-05-23",
	} {
		if !strings.Contains(recipe, exact) {
			t.Fatalf("OpenWrt 25.12.5 fstools recipe lacks %q", exact)
		}
	}

	mountRoot := pinnedSource(t, fstoolsRoot, "mount_root.c")
	requireOrdered(t, mountRoot,
		`case FS_NONE:`, `return ramoverlay();`,
		`case FS_EXT4:`, `case FS_F2FS:`, `case FS_JFFS2:`, `case FS_UBIFS:`, `mount_overlay(data);`)

	mount := pinnedSource(t, fstoolsRoot, "libfstools/mount.c")
	for _, exact := range []string{
		`snprintf(overlay, sizeof(overlay), "overlayfs:%s", rw_root);`,
		`snprintf(upperdir, sizeof(upperdir), "%s/upper", rw_root);`,
		`snprintf(workdir, sizeof(workdir), "%s/work", rw_root);`,
		`mount("tmpfs", "/tmp/root", "tmpfs", MS_NOATIME, "mode=0755");`,
		`return fopivot("/tmp/root", "/rom");`,
	} {
		if !strings.Contains(mount, exact) {
			t.Fatalf("pinned fstools mount semantics lack %q", exact)
		}
	}

	overlay := pinnedSource(t, fstoolsRoot, "libfstools/overlay.c")
	requireOrdered(t, overlay,
		`const char *overlay_mp = "/tmp/overlay";`,
		`err = overlay_mount_fs(v, overlay_mp);`,
		`mount_move("/tmp", "", "/overlay")`,
		`fopivot("/overlay", "/rom")`,
		`fallback to ramoverlay`,
		`return ramoverlay();`)

	err := filepath.WalkDir(fstoolsRoot, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(strings.ToLower(string(data)), "fiemap") {
			t.Errorf("pinned fstools unexpectedly uses FIEMAP in %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPinnedOpenWrtXFSAvailabilityMatchesApprovedPersistencePolicy(t *testing.T) {
	workspaceRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	openwrtRoot := filepath.Join(workspaceRoot, "upstreams", "openwrt-openwrt-25.12.5")
	requireGitRevision(t, openwrtRoot, pinnedOpenWrtCommit)

	filesystems := pinnedSource(t, openwrtRoot, "package/kernel/linux/modules/fs.mk")
	xfs := definitionBody(t, filesystems, "KernelPackage/fs-xfs")
	if !strings.Contains(xfs, "KCONFIG:=CONFIG_XFS_FS") || !strings.Contains(xfs, "FILES:=$(LINUX_DIR)/fs/xfs/xfs.ko") {
		t.Fatalf("locked OpenWrt XFS package contract changed:\n%s", xfs)
	}
	if !ApprovedPersistentFilesystem("xfs") {
		t.Fatal("locked OpenWrt XFS capability is absent from the approved persistence policy")
	}
	for _, filesystem := range []string{"ntfs", "ntfs3"} {
		if ApprovedPersistentFilesystem(filesystem) {
			t.Fatalf("unapproved filesystem %s entered the OpenWrt persistence policy", filesystem)
		}
	}
}

func requireGitRevision(t testing.TB, root, expected string) {
	t.Helper()
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-c", "safe.directory="+absolute, "-C", absolute, "rev-parse", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != expected {
		t.Fatalf("source revision at %s = %q, want %s: %v", absolute, strings.TrimSpace(string(output)), expected, err)
	}
}
