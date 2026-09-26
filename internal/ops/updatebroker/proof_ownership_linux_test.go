//go:build linux

package updatebroker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestNativeUpdatePreservesSSHProofOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("native update ownership gate requires root")
	}
	root, err := os.MkdirTemp("/run", "solovey-proof-update-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for _, profile := range []string{"full", "core"} {
		t.Run(profile, func(t *testing.T) {
			release := filepath.Join(root, profile)
			for _, name := range []string{"solovey-ui", "solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"} {
				publicationExecutable(t, filepath.Join(release, name), name)
			}
			if profile == "full" {
				publicationExecutable(t, filepath.Join(release, "solovey-owner-manifest"), "owner")
			}
			manifest := publicationManifest(t, filepath.Join(release, "solovey-ui"))
			proof := manifest.Clients[0]
			proof.Name = "ssh-proof"
			proof.UID = 0
			proof.GID = 0
			proof.AnyNonRootUID = true
			proof.AnyGID = true
			proof.RequiredGroup = 12345
			proof.CgroupUnit = ""
			proof.Roles = []broker.Role{broker.RoleSSHProof}
			manifest.Clients = append(manifest.Clients, proof)
			manifest, err = broker.FinalizeManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "broker-clients.json")
			if err := os.WriteFile(path, raw, 0o640); err != nil {
				t.Fatal(err)
			}
			host := &Host{Manifest: path, atomicOps: productionUpdateAtomicOps}
			if err := host.verifyPreparedReleaseExecutables(release, profile); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ { // activation and idempotent replay/rollback
				if err := host.rewriteClientManifest(release); err != nil {
					t.Fatal(err)
				}
				if err := host.verifyPreparedReleaseExecutables(release, profile); err != nil {
					t.Fatal(err)
				}
			}
			info, err := os.Stat(filepath.Join(release, "solovey-ssh-proof"))
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if stat.Uid != 0 || stat.Gid != 12345 || info.Mode()&(os.ModePerm|os.ModeSetgid|os.ModeSetuid) != broker.SystemdSSHProofMode {
				t.Fatalf("proof uid=%d gid=%d mode=%v", stat.Uid, stat.Gid, info.Mode())
			}
			if err := os.Chown(filepath.Join(release, "solovey-ssh-proof"), 0, 12347); err != nil {
				t.Fatal(err)
			}
			if err := host.rewriteClientManifest(release); err == nil {
				t.Fatal("wrong group silently repaired")
			}
		})
	}
}
