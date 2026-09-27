//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRootIdentityInputs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real root ownership required")
	}
	for _, item := range []struct {
		name string
		mode os.FileMode
	}{{"deployment-profile", 0o644}, {"BUILD_INFO.txt", 0o644}, {"instance-id", 0o400}} {
		t.Run(item.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), item.name)
			if err := atomicRootFile(path, []byte("identity-test\n"), item.mode); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("actual stat type %T", info.Sys())
			if _, err := boundedRootFile(path, 128, item.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := regularRootDigest(path, 128); err != nil {
				t.Fatal(err)
			}
			for _, owner := range [][2]int{{0, 65534}, {65534, 0}} {
				if err := os.Chown(path, owner[0], owner[1]); err != nil {
					t.Fatal(err)
				}
				_, err := boundedRootFile(path, 128, item.mode)
				if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "expected uid=0 gid=0") || strings.Contains(err.Error(), "identity-test") {
					t.Fatalf("unsafe ownership diagnostic: %v", err)
				}
				if _, err := regularRootDigest(path, 128); err == nil {
					t.Fatal("unsafe digest owner accepted")
				}
			}
			if err := os.Chown(path, 0, 0); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o666); err != nil {
				t.Fatal(err)
			}
			if _, err := boundedRootFile(path, 128, item.mode); err == nil {
				t.Fatal("unsafe mode accepted")
			}
		})
	}
}

func TestIdentityPublicationIgnoresUmaskAndSetgid(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real root ownership required")
	}
	dir := t.TempDir()
	if err := os.Chown(dir, 0, 65534); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, os.ModeSetgid|0o750); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := filepath.Join(dir, "instance-id")
	if err := atomicRootFile(path, []byte("identity-test\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedRootFile(path, 128, 0o400); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual shell producer, including replacement of an old marker.
	installer, err := filepath.Abs("../../../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"native-hardened", "native-legacy-root"} {
		command := exec.Command("bash", "-c", `source "$1"; SYSTEMD_PROFILE_ROOT="$2"; SYSTEMD_SERVICE="$2/service"; DEPLOYMENT_MARKER="$2/deployment-profile"; DEPLOYMENT_PROFILE="$3"; touch "$2/solovey-ui-$3.service"; configure_deployment_profile`, "test", installer, dir, profile)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("profile producer: %v %s", err, output)
		}
		data, err := boundedRootFile(filepath.Join(dir, "deployment-profile"), 128, 0o644)
		if err != nil || strings.TrimSpace(string(data)) != profile {
			t.Fatalf("profile identity: %q %v", data, err)
		}
	}
}
