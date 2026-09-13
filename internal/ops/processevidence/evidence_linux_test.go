//go:build linux

package processevidence

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestObserveBindsCurrentMappedExecutableObject(t *testing.T) {
	fact, err := Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if fact.ProviderRevision != RevisionV2 || fact.ExeDevice == 0 || fact.ExeInode == 0 || fact.ExeSize <= 0 ||
		len(fact.ExeDigest) != 64 || fact.ExeMode&0o111 == 0 || len(fact.CgroupRevision) != 64 {
		t.Fatalf("incomplete process evidence: %+v", fact)
	}
	if fact.CgroupAvailability != CgroupAvailable && fact.CgroupAvailability != CgroupUnavailable {
		t.Fatalf("unexpected cgroup state: %q", fact.CgroupAvailability)
	}
}

func TestCoreProcessEvidenceSurvivesAbsentOptionalCgroupFile(t *testing.T) {
	root := t.TempDir()
	pid := 77
	dir := filepath.Join(root, "77")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), stat, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status"), status, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}
	fact, err := observeAt(root, pid)
	if err != nil {
		t.Fatal(err)
	}
	if fact.CgroupAvailability != CgroupUnavailable || len(fact.Cgroups) != 0 || fact.ExeDigest == "" {
		t.Fatalf("optional cgroup absence contaminated core evidence: %+v", fact)
	}
}

func TestMalformedCgroupIsExplicitWithoutErasingCoreEvidence(t *testing.T) {
	root := t.TempDir()
	pid := 78
	dir := filepath.Join(root, "78")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.ReadFile("/proc/self/stat")
	status, _ := os.ReadFile("/proc/self/status")
	executable, _ := os.Executable()
	for name, data := range map[string][]byte{"stat": stat, "status": status, "cgroup": []byte("0::relative\n")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(executable, filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}
	fact, err := observeAt(root, pid)
	if err != nil {
		t.Fatal(err)
	}
	if fact.CgroupAvailability != CgroupMalformed || fact.ExeDigest == "" {
		t.Fatalf("malformed cgroup was not isolated: %+v", fact)
	}
}

func TestRunningMappedExecutableObjectSurvivesPathReplacementAndUnlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mapped-peer.test")
	copyProcessFixture(t, os.Args[0], path)
	command := exec.Command(path, "-test.run=^TestMappedExecutableObjectHelper$")
	command.Env = append(os.Environ(), "SUI_MAPPED_EXECUTABLE_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	before, err := Observe(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	mapped := filepath.Join(root, "mapped-original.test")
	if err := os.Rename(path, mapped); err != nil {
		t.Fatal(err)
	}
	copyProcessFixture(t, os.Args[0], path)
	after, err := Observe(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if after.ExeDevice != before.ExeDevice || after.ExeInode != before.ExeInode || after.ExeDigest != before.ExeDigest ||
		after.Executable == path {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	if err := os.Remove(mapped); err != nil {
		t.Fatal(err)
	}
	deleted, err := Observe(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.ExecutableDeleted || deleted.ExeDevice != before.ExeDevice || deleted.ExeInode != before.ExeInode || deleted.ExeDigest != before.ExeDigest {
		t.Fatalf("deleted mapped object=%+v", deleted)
	}
}

func TestMappedExecutableObjectHelper(t *testing.T) {
	if os.Getenv("SUI_MAPPED_EXECUTABLE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(10 * time.Second)
}

func copyProcessFixture(t *testing.T, source, target string) {
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
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
