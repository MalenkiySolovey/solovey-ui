//go:build linux

package updatebroker

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// Archive bytes are published root:root/0755. Activation owns the one transition
// to the dedicated socket group, before publishing the new client manifest.
func openDeployedClient(path string, entry broker.ClientManifest) (*executableobject.Object, error) {
	policy, err := broker.SystemdClientExecutablePolicy(entry)
	if err != nil {
		return nil, err
	}
	object, err := executableobject.Open(path, policy)
	if err == nil || policy.RequiredOwner == nil {
		return object, err
	}
	prepared := policy
	prepared.RequiredOwner, prepared.RequireRootOwner, prepared.RequiredMode = nil, true, 0o755
	prepared.AllowSymlink = false
	object, err = executableobject.Open(path, prepared)
	if err != nil {
		return nil, err
	}
	before := object.Identity()
	file := object.File()
	err = file.Chown(0, int(policy.RequiredOwner.GID))
	if err == nil {
		err = file.Chmod(policy.RequiredMode)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, object.Close())
	if err != nil {
		return nil, err
	}
	object, err = executableobject.Open(path, policy)
	if err != nil {
		return nil, err
	}
	after := object.Identity()
	if before.Device != after.Device || before.Inode != after.Inode || before.Digest != after.Digest {
		_ = object.Close()
		return nil, errors.New("native SSH proof object changed during deployment")
	}
	return object, nil
}

func (h *Host) verifyReleaseExecutables(root string) error {
	for _, name := range []string{"solovey-ui", "solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"} {
		path := filepath.Join(root, name)
		if safeReleaseExecutable(path) {
			continue
		}
		if name != "solovey-ssh-proof" {
			return fmt.Errorf("release executable %s is unsafe", name)
		}
		manifest, err := broker.LoadManifest(h.Manifest)
		if err != nil || manifest.Schema != broker.ManifestSchemaSystemd {
			return errors.New("native proof group authority is unavailable")
		}
		var proof *broker.ClientManifest
		for i := range manifest.Clients {
			for _, role := range manifest.Clients[i].Roles {
				if role != broker.RoleSSHProof {
					continue
				}
				if proof != nil {
					return errors.New("native proof group authority is ambiguous")
				}
				proof = &manifest.Clients[i]
			}
		}
		if proof == nil {
			return errors.New("native proof group authority is absent")
		}
		policy, err := broker.SystemdClientExecutablePolicy(*proof)
		if err != nil {
			return err
		}
		object, err := executableobject.Open(path, policy)
		if err != nil {
			return err
		}
		if err := object.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) verifyPreparedReleaseExecutables(root, profile string) error {
	if err := h.verifyReleaseExecutables(root); err != nil {
		return err
	}
	return verifyReleaseOwnerWriter(root, profile)
}
