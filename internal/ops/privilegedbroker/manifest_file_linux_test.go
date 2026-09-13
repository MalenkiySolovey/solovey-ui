//go:build linux

package privilegedbroker

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRuntimeManifestTrustMatrix(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned manifest trust matrix requires root")
	}
	t.Run("strict root-owned parent", func(t *testing.T) {
		_, parent, path := runtimeManifestFixture(t, 0o711, 0)
		if _, err := LoadManifest(path); err != nil {
			t.Fatalf("safe manifest under %s was rejected: %v", parent, err)
		}
	})
	t.Run("OpenWrt runtime shape is independent of configuration ancestor mode", func(t *testing.T) {
		base, _, path := runtimeManifestFixture(t, 0o750, 65534)
		configurationRoot := filepath.Join(base, "etc")
		if err := os.Mkdir(configurationRoot, 0o775); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(configurationRoot, 0o775); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(path); err != nil {
			t.Fatalf("runtime authority depended on unrelated platform configuration mode: %v", err)
		}
	})
	for name, mode := range map[string]os.FileMode{"group writable parent": 0o770, "world writable parent": 0o752} {
		t.Run(name, func(t *testing.T) {
			_, _, path := runtimeManifestFixture(t, mode, 65534)
			if _, err := LoadManifest(path); err == nil {
				t.Fatal("unsafe Solovey-owned manifest parent was accepted")
			}
		})
	}
	for name, mode := range map[string]os.FileMode{"group writable manifest": 0o660, "world writable manifest": 0o642} {
		t.Run(name, func(t *testing.T) {
			_, _, path := runtimeManifestFixture(t, 0o750, 65534)
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(path); err == nil {
				t.Fatal("writable broker manifest was accepted")
			}
		})
	}
	t.Run("manifest symlink", func(t *testing.T) {
		_, _, path := runtimeManifestFixture(t, 0o750, 65534)
		target := path + ".target"
		if err := os.Rename(path, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(path); err == nil {
			t.Fatal("broker manifest symlink was accepted")
		}
	})
	t.Run("parent symlink", func(t *testing.T) {
		base, parent, path := runtimeManifestFixture(t, 0o750, 65534)
		alias := filepath.Join(base, "alias")
		if err := os.Symlink(parent, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(filepath.Join(alias, filepath.Base(path))); err != nil {
			t.Fatalf("root-owned manifest parent alias was rejected: %v", err)
		}
	})
	t.Run("trusted OpenWrt multi-hop alias", func(t *testing.T) {
		logical, canonical := writeAliasedManifestFixture(t, false)
		loaded, err := LoadManifest(logical)
		if err != nil {
			t.Fatalf("OpenWrt-shaped alias was rejected: %v", err)
		}
		if loaded.Revision == "" || canonical == "" {
			t.Fatal("aliased manifest fixture did not produce a canonical object")
		}
	})
	t.Run("alias retargets to untrusted canonical ancestry", func(t *testing.T) {
		logical, _ := writeAliasedManifestFixture(t, true)
		if _, err := LoadManifest(logical); err == nil {
			t.Fatal("alias into writable canonical ancestry was accepted")
		}
	})
	t.Run("alias owner is untrusted", func(t *testing.T) {
		logical, _ := writeAliasedManifestFixture(t, false)
		alias := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(logical))), "run")
		if err := os.Lchown(alias, 65534, 65534); err != nil {
			t.Skipf("symlink ownership mutation unavailable: %v", err)
		}
		if _, err := LoadManifest(logical); err == nil {
			t.Fatal("non-root-owned logical alias was accepted")
		}
	})
	t.Run("wrong manifest owner", func(t *testing.T) {
		_, _, path := runtimeManifestFixture(t, 0o750, 65534)
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(path); err == nil {
			t.Fatal("non-root broker manifest was accepted")
		}
	})
	t.Run("manifest object replacement during authenticated read transaction", func(t *testing.T) {
		_, _, path := runtimeManifestFixture(t, 0o750, 65534)
		object, err := openTrustedManifestObject(path)
		if err != nil {
			t.Fatal(err)
		}
		defer object.Close()
		if err := os.Rename(path, path+".authenticated"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("replacement"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 0, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := readTrustedManifestObject(object); err == nil {
			t.Fatal("manifest path replacement was not fenced by post-read revalidation")
		}
	})
	t.Run("ambiguous platform write authority fails closed", func(t *testing.T) {
		base, parent, path := runtimeManifestFixture(t, 0o750, 65534)
		if err := os.Chmod(base, 0o770); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(path); err == nil {
			t.Fatalf("manifest under writable platform ancestor was accepted: %s", parent)
		}
	})
}

// TestTrustedCanonicalAliasManifestLoad models the pinned OpenWrt logical
// runtime label through a root-owned /run -> /var/run -> /tmp/run alias chain,
// including stock OpenWrt's root-owned 01777 /tmp boundary.
func TestTrustedCanonicalAliasManifestLoad(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned manifest alias fixture requires root")
	}
	root, err := os.MkdirTemp("/root", "solovey-manifest-alias-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	canonicalDir := filepath.Join(root, "tmp", "run", "solovey-ui")
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "tmp"), os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "tmp"), filepath.Join(root, "var")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "var", "run"), filepath.Join(root, "run")); err != nil {
		t.Fatal(err)
	}
	logical := filepath.Join(root, "run", "solovey-ui", "broker-clients.json")
	canonical := filepath.Join(root, "tmp", "run", "solovey-ui", "broker-clients.json")
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{{
		Name: "panel", UID: 1000, GID: 1000, Executable: "/usr/lib/solovey-ui/solovey-ui",
		ExecutableDigest: strings.Repeat("a", 64), Device: 1, Inode: 2,
		CgroupUnit: "solovey-ui.service", CgroupPolicy: CgroupRequired,
		CgroupAuthorityRevision: CgroupAuthorityRevisionV1, Roles: []Role{RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, append(data, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(canonical, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(canonical, 0, 0); err != nil {
		t.Fatal(err)
	}
	loaded, loadErr := LoadManifest(logical)
	t.Logf("CURRENT_LOGICAL_PATH: %s", logical)
	t.Logf("CURRENT_CANONICAL_PATH: %s", canonical)
	t.Logf("CURRENT_FILE_IDENTITY: root-owned regular 0640 manifest at %s", canonical)
	t.Logf("CURRENT_LOGICAL_ANCESTRY: %s/run(symlink) -> %s/var/run(symlink) -> solovey-ui(directory)", root, root)
	t.Logf("CURRENT_CANONICAL_ANCESTRY: %s/tmp (root-owned 01777) -> run/solovey-ui (root-owned, non-writable)", root)
	if loadErr != nil {
		t.Fatalf("CURRENT_LOAD_RESULT: FAIL: %v", loadErr)
	}
	if loaded.Revision != manifest.Revision {
		t.Fatalf("loaded revision changed: got %q want %q", loaded.Revision, manifest.Revision)
	}
	t.Log("CORRECTED_LOAD_RESULT: PASS")
}

func TestUnprivilegedManifestSubstitutionIsRejected(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential transition requires root")
	}
	base, _, path := runtimeManifestFixture(t, 0o750, 65534)
	helper := filepath.Join(base, "manifest-substitution-helper")
	copyManifestTestHelper(t, helper)
	command := exec.Command(helper, "-test.run=^TestManifestSubstitutionHelper$")
	command.Env = []string{"SUI_MANIFEST_SUBSTITUTION_PATH=" + path}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged substitution assertion failed: %v: %s", err, output)
	}
	if _, err := LoadManifest(path); err != nil {
		t.Fatalf("original manifest changed after rejected substitution: %v", err)
	}
}

func copyManifestTestHelper(t *testing.T, path string) {
	t.Helper()
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(errors.Join(copyErr, closeErr))
	}
}

func TestManifestSubstitutionHelper(t *testing.T) {
	path := os.Getenv("SUI_MANIFEST_SUBSTITUTION_PATH")
	if path == "" {
		t.Skip("helper process only")
	}
	if err := os.Rename(path, path+".attacker"); err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unprivileged rename error=%v", err)
	}
	if err := os.WriteFile(path, []byte("attacker"), 0o640); err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unprivileged content replacement error=%v", err)
	}
}

func runtimeManifestFixture(t *testing.T, parentMode os.FileMode, parentGID int) (string, string, string) {
	t.Helper()
	base, err := os.MkdirTemp("/run", "solovey-manifest-policy-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	platformRun := filepath.Join(base, "run")
	parent := filepath.Join(platformRun, "solovey-ui")
	if err := os.Mkdir(platformRun, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, parentMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 0, parentGID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, parentMode); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "broker-clients.json")
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{{
		Name: "panel", UID: 1000, GID: 1000, Executable: "/usr/lib/solovey-ui/solovey-ui",
		ExecutableDigest: strings.Repeat("a", 64), Device: 1, Inode: 2,
		CgroupUnit: "solovey-ui.service", CgroupPolicy: CgroupRequired,
		CgroupAuthorityRevision: CgroupAuthorityRevisionV1, Roles: []Role{RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	return base, parent, path
}

// writeAliasedManifestFixture creates a root-owned logical alias chain and
// returns the logical label plus the canonical file path. When untrusted is
// true, the alias targets a world-writable canonical parent so the canonical
// object policy must reject it even though the logical link itself is root
// owned.
func writeAliasedManifestFixture(t *testing.T, untrusted bool) (string, string) {
	t.Helper()
	root, err := os.MkdirTemp("/root", "solovey-manifest-openwrt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	canonicalBase := filepath.Join(root, "tmp")
	canonicalDir := filepath.Join(canonicalBase, "run", "solovey-ui")
	if untrusted {
		canonicalBase = filepath.Join(root, "attacker")
		canonicalDir = filepath.Join(canonicalBase, "run", "solovey-ui")
	}
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if untrusted {
		if err := os.Chmod(canonicalBase, 0o777); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.Chmod(canonicalBase, os.ModeSticky|0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "tmp"), filepath.Join(root, "var")); err != nil {
			t.Fatal(err)
		}
	}
	if untrusted {
		if err := os.Symlink(canonicalBase, filepath.Join(root, "var")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "var", "run"), filepath.Join(root, "run")); err != nil {
		t.Fatal(err)
	}
	logical := filepath.Join(root, "run", "solovey-ui", "broker-clients.json")
	canonical := filepath.Join(canonicalDir, "broker-clients.json")
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{{
		Name: "panel", UID: 1000, GID: 1000, Executable: "/usr/lib/solovey-ui/solovey-ui",
		ExecutableDigest: strings.Repeat("a", 64), Device: 1, Inode: 2,
		CgroupUnit: "solovey-ui.service", CgroupPolicy: CgroupRequired,
		CgroupAuthorityRevision: CgroupAuthorityRevisionV1, Roles: []Role{RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, append(data, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(canonical, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(canonical, 0o640); err != nil {
		t.Fatal(err)
	}
	return logical, canonical
}
