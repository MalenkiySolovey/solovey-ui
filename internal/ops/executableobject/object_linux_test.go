//go:build linux

package executableobject

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestOpenBindsDigestAndSurvivesPathReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("original"), 0o700); err != nil {
		t.Fatal(err)
	}
	object, err := Open(path, Policy{RequireRegular: true, RequireExecutable: true, ForbiddenMode: 0o022, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	if object.Identity().Digest == "" || object.Identity().Inode == 0 {
		t.Fatalf("incomplete identity: %+v", object.Identity())
	}
	if err := os.Rename(path, filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := object.Revalidate(); err != nil {
		t.Fatalf("opened object was not stable after path replacement: %v", err)
	}
	if object.Identity().ResolvedPath != path {
		// The label remains the owner-selected path; resolution is only a diagnostic label.
		t.Fatalf("unexpected resolved path: %q", object.Identity().ResolvedPath)
	}
}

func TestStablePathPolicyRejectsReplacementAndAliasRetarget(t *testing.T) {
	t.Run("leaf replacement", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest")
		if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		object, err := Open(path, Policy{RequireRegular: true, ForbiddenMode: 0o022, MaxBytes: 1024, RequireStablePath: true})
		if err != nil {
			t.Fatal(err)
		}
		defer object.Close()
		if err := os.Rename(path, path+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := object.Revalidate(); err == nil {
			t.Fatal("stable logical label accepted a replacement object")
		}
	})
	t.Run("ancestor alias retarget", func(t *testing.T) {
		root := t.TempDir()
		for _, directory := range []string{"one/runtime", "two/runtime"} {
			if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, directory, "manifest"), []byte(directory), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(filepath.Join(root, "one"), alias); err != nil {
			t.Fatal(err)
		}
		label := filepath.Join(alias, "runtime", "manifest")
		object, err := Open(label, Policy{RequireRegular: true, ForbiddenMode: 0o022, MaxBytes: 1024, RequireStablePath: true})
		if err != nil {
			t.Fatal(err)
		}
		defer object.Close()
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "two"), alias); err != nil {
			t.Fatal(err)
		}
		if err := object.Revalidate(); err == nil {
			t.Fatal("stable logical label accepted an ancestor alias retarget")
		}
	})
}

func TestOpenRejectsWritableAndNonExecutableObjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, Policy{RequireRegular: true, RequireExecutable: true, ForbiddenMode: 0o022}); err == nil {
		t.Fatal("non-executable object unexpectedly trusted")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, Policy{RequireRegular: true, RequireExecutable: true, ForbiddenMode: 0o022}); err == nil {
		t.Fatal("group-writable object unexpectedly trusted")
	}
}

func TestExecutionUsesTheVerifiedObjectAfterPathReplacement(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	object, err := Open(path, Policy{RequireRegular: true, RequireExecutable: true, ForbiddenMode: 0o022})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	if err := os.Rename(path, filepath.Join(dir, "verified")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement must not execute"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(object.ExecPath(0), "-test.run=^TestVerifiedObjectChild$", "-test.v")
	command.Args[0] = object.Label()
	command.ExtraFiles = []*os.File{object.File()}
	command.Env = []string{"SUI_EXECUTABLE_OBJECT_CHILD=1"}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("verified descriptor execution failed: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "verified-object-child") {
		t.Fatalf("verified object did not run: %s", output)
	}
}

func TestVerifiedObjectChild(t *testing.T) {
	if os.Getenv("SUI_EXECUTABLE_OBJECT_CHILD") != "1" {
		t.Skip("helper process only")
	}
	t.Log("verified-object-child")
}

func TestRootExecutableObjectTrustMatrixOnLinuxFilesystem(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root ownership matrix requires root")
	}
	root, err := os.MkdirTemp("/run", "solovey-executable-object-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{MaxBytes: 512 << 20, RequireRegular: true, RequireExecutable: true, RequireRootOwner: true,
		ForbiddenMode: 0o022, RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022}
	write := func(name string, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, payload, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write("good", 0o555)
	object, err := Open(good, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	if identity := object.Identity(); identity.UID != 0 || identity.GID != 0 || identity.Mode.Perm() != 0o555 || identity.Digest == "" {
		t.Fatalf("identity=%+v", identity)
	}
	for _, test := range []struct {
		name  string
		mode  os.FileMode
		chown bool
	}{
		{"non_root_owned", 0o555, true},
		{"group_writable", 0o575, false},
		{"world_writable", 0o557, false},
		{"missing_execute", 0o444, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := write(test.name, test.mode)
			if test.chown {
				if err := os.Chown(path, 65534, 65534); err != nil {
					t.Fatal(err)
				}
			}
			if selected, err := Open(path, policy); err == nil {
				_ = selected.Close()
				t.Fatal("unsafe executable object was accepted")
			}
		})
	}
	unsafeAncestor := filepath.Join(root, "unsafe-ancestor")
	if err := os.Mkdir(unsafeAncestor, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeAncestor, 0o777); err != nil {
		t.Fatal(err)
	}
	unsafePath := filepath.Join(unsafeAncestor, "tool")
	if err := os.WriteFile(unsafePath, payload, 0o555); err != nil {
		t.Fatal(err)
	}
	if selected, err := Open(unsafePath, policy); err == nil {
		_ = selected.Close()
		t.Fatal("unsafe executable ancestry was accepted")
	}
	alias := filepath.Join(root, "allowed-alias")
	if err := os.Symlink(good, alias); err != nil {
		t.Fatal(err)
	}
	if selected, err := Open(alias, policy); err == nil {
		_ = selected.Close()
		t.Fatal("symlink was accepted without owner policy")
	}
	symlinkPolicy := policy
	symlinkPolicy.AllowSymlink = true
	aliasObject, err := Open(alias, symlinkPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if aliasObject.Identity().Inode != object.Identity().Inode {
		t.Fatal("safe owner-selected symlink did not bind its target object")
	}
	_ = aliasObject.Close()
	unsafeAlias := filepath.Join(root, "unsafe-alias")
	if err := os.Symlink(unsafePath, unsafeAlias); err != nil {
		t.Fatal(err)
	}
	if selected, err := Open(unsafeAlias, symlinkPolicy); err == nil {
		_ = selected.Close()
		t.Fatal("symlink to unsafe ancestry was accepted")
	}

	// Execute the already trusted root-owned fixture. The Go build cache may be
	// beneath a root-only directory and is not part of the ancestry proposition
	// this unprivileged replacement check exercises.
	command := exec.Command(good, "-test.run=^TestUnprivilegedContentReplacementHelper$")
	command.Env = []string{"SUI_UNPRIVILEGED_REPLACEMENT=" + good}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged replacement assertion failed: %v: %s", err, output)
	}

	verifiedPath := filepath.Join(root, "verified")
	if err := os.Rename(good, verifiedPath); err != nil {
		t.Fatal(err)
	}
	replacement := write("good", 0o555)
	replacementInfo, err := os.Stat(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if replacementInfo.Sys() == nil || object.Identity().Inode == fileInode(t, replacementInfo) {
		t.Fatal("pathname replacement did not produce a distinct object")
	}
	if err := object.Revalidate(); err != nil {
		t.Fatal(err)
	}
	os.Setenv("LD_PRELOAD", "/attacker/injected.so")
	os.Setenv("PYTHONPATH", "/attacker/python")
	t.Cleanup(func() {
		_ = os.Unsetenv("LD_PRELOAD")
		_ = os.Unsetenv("PYTHONPATH")
	})
	executed := exec.Command(object.ExecPath(0), "-test.run=^TestFixedExecutableEnvironmentChild$", "-test.v")
	executed.Args[0] = object.Label()
	executed.ExtraFiles = []*os.File{object.File()}
	executed.Env = []string{"LANG=C", "LC_ALL=C", "SUI_FIXED_EXECUTABLE_ENV=1"}
	output, err := executed.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("fixed-environment-child")) {
		t.Fatalf("descriptor execution failed: %v: %s", err, output)
	}
}

func TestRootOwnedStickyAncestryRequiresExplicitNarrowPolicy(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root ownership matrix requires root")
	}
	root, err := os.MkdirTemp("/root", "solovey-sticky-ancestry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := Policy{
		MaxBytes: 1024, RequireRegular: true, RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
	}
	fixture := func(name string, boundaryMode os.FileMode, boundaryUID int) string {
		t.Helper()
		boundary := filepath.Join(root, name)
		child := filepath.Join(boundary, "run", "solovey-ui")
		if err := os.MkdirAll(child, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(boundary, boundaryMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(boundary, boundaryUID, boundaryUID); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(child, "broker-clients.json")
		if err := os.WriteFile(path, []byte("manifest"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 0, 0); err != nil {
			t.Fatal(err)
		}
		return path
	}

	stockOpenWrt := fixture("stock-openwrt-tmp", os.ModeSticky|0o777, 0)
	if object, err := Open(stockOpenWrt, policy); err == nil {
		_ = object.Close()
		t.Fatal("the stronger default executable policy accepted writable sticky ancestry")
	}
	manifestPolicy := policy
	manifestPolicy.AllowRootOwnedStickyAncestry = true
	object, err := Open(stockOpenWrt, manifestPolicy)
	if err != nil {
		t.Fatalf("the explicit manifest policy rejected root-owned 01777 ancestry: %v", err)
	}
	_ = object.Close()

	for _, test := range []struct {
		name string
		mode os.FileMode
		uid  int
	}{
		{name: "root-non-sticky-0777", mode: 0o777, uid: 0},
		{name: "root-non-sticky-0775", mode: 0o775, uid: 0},
		{name: "non-root-sticky-01777", mode: os.ModeSticky | 0o777, uid: 65534},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := fixture(test.name, test.mode, test.uid)
			if selected, err := Open(path, manifestPolicy); err == nil {
				_ = selected.Close()
				t.Fatal("unsafe ancestry was accepted by the sticky-boundary policy")
			}
		})
	}
}

func TestUnprivilegedContentReplacementHelper(t *testing.T) {
	path := os.Getenv("SUI_UNPRIVILEGED_REPLACEMENT")
	if path == "" {
		t.Skip("helper process only")
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = file.Close()
		t.Fatal("unprivileged content replacement unexpectedly succeeded")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("content replacement error=%v", err)
	}
}

func TestFixedExecutableEnvironmentChild(t *testing.T) {
	if os.Getenv("SUI_FIXED_EXECUTABLE_ENV") != "1" {
		t.Skip("helper process only")
	}
	if os.Getenv("LANG") != "C" || os.Getenv("LC_ALL") != "C" || os.Getenv("LD_PRELOAD") != "" || os.Getenv("PYTHONPATH") != "" || os.Getenv("PATH") != "" {
		t.Fatalf("child environment=%q", os.Environ())
	}
	t.Log("fixed-environment-child")
}

func fileInode(t *testing.T, info os.FileInfo) uint64 {
	t.Helper()
	value, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("filesystem identity is unavailable")
	}
	return value.Ino
}
