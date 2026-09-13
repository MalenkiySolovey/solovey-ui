//go:build linux

package systemdexec

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemctlOwnerSelectsOneSafeObjectAndRejectsAmbiguity(t *testing.T) {
	first, root := ownerExecutableFixture(t, "systemctl-one")
	alias := filepath.Join(root, "systemctl-alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	object, err := openCandidates([]string{first, alias})
	if err != nil || object.Identity().Inode == 0 {
		t.Fatalf("object=%+v err=%v", object, err)
	}
	_ = object.Close()
	second, _ := ownerExecutableFixtureAt(t, root, "systemctl-two")
	if object, err := openCandidates([]string{first, second}); err == nil || object != nil {
		if object != nil {
			_ = object.Close()
		}
		t.Fatal("distinct systemctl candidates were accepted as one authority")
	}
}

func ownerExecutableFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux executable fixture is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-systemdexec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path, _ := ownerExecutableFixtureAt(t, root, name)
	return path, root
}

func ownerExecutableFixtureAt(t *testing.T, root, name string) (string, string) {
	t.Helper()
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	path := filepath.Join(root, name)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return path, root
}
