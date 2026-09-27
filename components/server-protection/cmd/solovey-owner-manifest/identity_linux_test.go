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

// Run the complete installed identity chain in a child chroot, retaining real
// stat/chown and account lookup semantics without touching host product paths.
func TestInstalledNativeOwnerContract(t *testing.T) {
	if root := os.Getenv("SOLOVEY_OWNER_TEST_ROOT"); root != "" {
		if err := syscall.Chroot(root); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir("/"); err != nil {
			t.Fatal(err)
		}
		first, err := installedContract()
		if err != nil {
			t.Fatal(err)
		}
		second, err := installedContract()
		if err != nil {
			t.Fatal(err)
		}
		if first.InstanceID != second.InstanceID {
			t.Fatal("instance identity changed on replay")
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("real root-owned installation required")
	}
	for _, profile := range []string{"native-hardened", "native-legacy-root"} {
		t.Run(profile, func(t *testing.T) {
			root := t.TempDir()
			write := func(path, value string, mode os.FileMode) {
				t.Helper()
				path = filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := atomicRootFile(path, []byte(value), mode); err != nil {
					t.Fatal(err)
				}
			}
			write(profileMarker, profile+"\n", 0o644)
			write("/etc/passwd", "root:x:0:0:root:/root:/bin/sh\nsolovey-ui:x:12345:12345::/:/bin/false\n", 0o644)
			write("/etc/group", "root:x:0:\nsolovey-ui:x:12345:\n", 0o644)
			write(profileRoot+"/solovey-ui-"+profile+".service", "[Service]\nExecStart=/panel\n", 0o644)
			write(releaseRoot+"/fixture/BUILD_INFO.txt", "commit="+strings.Repeat("a", 40)+"\n", 0o644)
			write(releaseRoot+"/fixture/solovey-ui", "fixture executable identity\n", 0o755)
			if err := os.MkdirAll(filepath.Join(root, filepath.Dir(serviceLink)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(profileRoot+"/solovey-ui-"+profile+".service", filepath.Join(root, serviceLink)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("fixture", filepath.Join(root, currentRelease)); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestInstalledNativeOwnerContract$")
			// The instrumented child exits after chroot. Its coverage runtime
			// needs writable paths inside that filesystem, not the host go-build tree.
			if testing.CoverMode() != "" {
				if err := os.MkdirAll(filepath.Join(root, "tmp/coverage"), 0o755); err != nil {
					t.Fatal(err)
				}
				command.Args = append(command.Args, "-test.gocoverdir=/tmp/coverage")
			}
			command.Env = append(os.Environ(), "SOLOVEY_OWNER_TEST_ROOT="+root)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("installed contract: %v\n%s", err, output)
			}
		})
	}
}
