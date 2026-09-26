//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// This runs the installer operation and real manifest publication/reader. The
// old root:root validator rejected its output before a panel or DB could start.
func TestInstallerSSHProofOwnershipContract(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("canonical installer ownership gate requires root")
	}
	path := manifestWriterExecutableFixture(t)
	installer, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	const group = 12345
	command := exec.Command("bash", "-c", `source "$1"; install_ssh_proof_permissions "$2" "$3"`, "bash", installer, path, strconv.Itoa(group))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer permissions: %v: %s", err, output)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != 0 || stat.Gid != group || info.Mode()&(os.ModePerm|os.ModeSetgid|os.ModeSetuid|os.ModeSticky) != broker.SystemdSSHProofMode {
		t.Fatalf("installer object: uid=%d gid=%d mode=%v", stat.Uid, stat.Gid, info.Mode())
	}
	entry, err := client("ssh-proof", path, 0, group, true, broker.RoleSSHProof)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{entry}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestFile := filepath.Join(filepath.Dir(path), "broker-clients.json")
	if err := installManifest(manifestFile, data, realManifestFilesystem(), true); err != nil {
		t.Fatal(err)
	}
	loaded, err := broker.LoadManifest(manifestFile)
	if err != nil || loaded.Revision != manifest.Revision || loaded.Clients[0].RequiredGroup != group {
		t.Fatalf("manifest=%+v err=%v", loaded, err)
	}

	for _, bad := range []struct {
		name     string
		uid, gid int
		mode     os.FileMode
	}{
		{"non-root-owner", 12346, group, broker.SystemdSSHProofMode},
		{"setgid-root", 0, 0, broker.SystemdSSHProofMode},
		{"wrong-group", 0, 12346, broker.SystemdSSHProofMode},
		{"missing-setgid", 0, group, 0o755},
		{"group-writable", 0, group, os.ModeSetgid | 0o775},
		{"world-writable", 0, group, os.ModeSetgid | 0o757},
		{"setuid-root", 0, group, os.ModeSetuid | broker.SystemdSSHProofMode},
		{"sticky", 0, group, os.ModeSticky | broker.SystemdSSHProofMode},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if err := os.Chown(path, bad.uid, bad.gid); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, bad.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := client("ssh-proof", path, 0, group, true, broker.RoleSSHProof); err == nil {
				t.Fatal("unsafe proof object accepted")
			}
		})
	}
	if err := os.Chown(path, 0, group); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, broker.SystemdSSHProofMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := client("ssh-proof", path, 0, group, true, broker.RoleSSHProof); err == nil {
		t.Fatal("untrusted ancestry accepted")
	}
}
