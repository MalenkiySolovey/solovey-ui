//go:build linux

package sshbroker

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSSHExecutableOwnerBindsOneObjectAndRejectsDistinctCandidates(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux executable fixture is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-ssh-executable-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first := copySSHExecutable(t, root, "sshd-one")
	object, err := firstFixedBinary(first)
	if err != nil || object.Identity().Digest == "" {
		t.Fatalf("identity=%+v err=%v", object.Identity(), err)
	}
	_ = object.Close()
	alias := filepath.Join(root, "sshd-alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	if object, err := firstFixedBinary(alias); err == nil || object != nil {
		if object != nil {
			_ = object.Close()
		}
		t.Fatal("SSH owner unexpectedly permitted a symlink candidate")
	}
	second := copySSHExecutable(t, root, "sshd-two")
	if object, err := firstFixedBinary(first, second); err == nil || object != nil {
		if object != nil {
			_ = object.Close()
		}
		t.Fatal("distinct SSH executable candidates were accepted as one authority")
	}
}

func copySSHExecutable(t *testing.T, root, name string) string {
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
	return path
}
