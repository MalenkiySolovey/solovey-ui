//go:build linux

package sshbroker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestRealProcessFenceUsesRawExecutableContentIdentityAndObjectGeneration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned real Linux executable fixture is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-process-identity-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}

	path := copySSHExecutable(t, root, "ssh-daemon-fixture")
	object, err := firstFixedBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = object.Close() })
	identity := object.Identity()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Digest == "" || rawFileContentSHA256(raw) != identity.Digest || domain.Revision(raw) == identity.Digest {
		t.Fatalf("fixture did not preserve the physical R11 domain divergence: raw=%s json=%s", identity.Digest, domain.Revision(raw))
	}

	first := startProcessIdentityChild(t, object)
	firstRaw, err := processevidence.Observe(first.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := observeSSHProcess(context.Background(), first.Process.Pid, identity)
	if err != nil || projected.ExeDigest != identity.Digest || projected.ExeDevice != identity.Device || projected.ExeInode != identity.Inode {
		t.Fatalf("same raw-content executable object did not pass the production fence: process=%#v err=%v", projected, err)
	}
	stopProcessIdentityChild(t, first)

	second := startProcessIdentityChild(t, object)
	secondRaw, err := processevidence.Observe(second.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sameProcessGeneration(firstRaw, secondRaw) {
		t.Fatal("a changed real process generation was accepted as the prior generation")
	}

	replacementPath := copySSHExecutable(t, root, "replacement")
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatal(err)
	}
	replacement, err := firstFixedBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if replacement.Identity().Digest != identity.Digest ||
		replacement.Identity().Device == identity.Device && replacement.Identity().Inode == identity.Inode {
		t.Fatal("identical-byte pathname replacement did not create the required distinct object fixture")
	}
	if _, err := observeSSHProcess(context.Background(), second.Process.Pid, replacement.Identity()); err == nil {
		t.Fatal("a replaced executable object with identical bytes passed the production fence")
	}
	stopProcessIdentityChild(t, second)

	differentPath := filepath.Join(root, "different")
	writeDifferentExecutable(t, path, differentPath)
	different, err := firstFixedBinary(differentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer different.Close()
	differentChild := startProcessIdentityChild(t, different)
	defer stopProcessIdentityChild(t, differentChild)
	if different.Identity().Digest == identity.Digest {
		t.Fatal("different executable bytes unexpectedly retained the same raw digest")
	}
	expectedAtSameLabel := identity
	expectedAtSameLabel.Label = different.Identity().Label
	if _, err := observeSSHProcess(context.Background(), differentChild.Process.Pid, expectedAtSameLabel); err == nil {
		t.Fatal("different executable bytes passed the production process fence")
	}
}

func TestProcessIdentityContractChild(t *testing.T) {
	if os.Getenv("SOLOVEY_PROCESS_IDENTITY_CHILD") != "1" {
		return
	}
	fmt.Println("READY")
	_ = os.Stdout.Sync()
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func startProcessIdentityChild(t *testing.T, object *executableobject.Object) *exec.Cmd {
	t.Helper()
	command := exec.Command(object.ExecPath(0), "-test.run=^TestProcessIdentityContractChild$")
	command.Args[0] = object.Label()
	command.ExtraFiles = []*os.File{object.File()}
	command.Env = append(os.Environ(), "SOLOVEY_PROCESS_IDENTITY_CHILD=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	command.Cancel = func() error { return stdin.Close() }
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
			return
		}
		ready <- ""
	}()
	select {
	case value := <-ready:
		if value != "READY" {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("real process fixture did not become ready: %q", value)
		}
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal("real process fixture readiness timed out")
	}
	return command
}

func stopProcessIdentityChild(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command == nil || command.Process == nil || command.ProcessState != nil {
		return
	}
	if command.Cancel != nil {
		_ = command.Cancel()
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("real process fixture did not stop cleanly: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		<-done
		t.Fatal("real process fixture stop timed out")
	}
}

func writeDifferentExecutable(t *testing.T, source, target string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("different executable content")); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
