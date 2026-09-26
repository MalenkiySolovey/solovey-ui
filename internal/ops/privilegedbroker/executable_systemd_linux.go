//go:build linux

package privilegedbroker

import (
	"errors"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

const SystemdSSHProofMode = os.ModeSetgid | 0o755

// SystemdClientExecutablePolicy binds the native deployment's file authority.
// RequiredGroup is the dedicated socket group, never a grant of root GID.
// Other transports retain their own executable contract.
func SystemdClientExecutablePolicy(client ClientManifest) (executableobject.Policy, error) {
	policy := executableobject.Policy{MaxBytes: maxPeerExecutableBytes, AllowSymlink: true,
		RequireRegular: true, RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022, RequireStablePath: true}
	for _, role := range client.Roles {
		if role != RoleSSHProof {
			continue
		}
		if len(client.Roles) != 1 || !client.AnyNonRootUID || !client.AnyGID || client.RequiredGroup == 0 || client.UID != 0 || client.GID != 0 {
			return executableobject.Policy{}, errors.New("native SSH proof executable requires an exact non-root socket group")
		}
		policy.RequireRootOwner = false
		policy.RequiredOwner = &executableobject.Ownership{UID: 0, GID: client.RequiredGroup}
		policy.RequiredMode = SystemdSSHProofMode
	}
	return policy, nil
}

func (m Manifest) peerExecutablePolicy(role Role) (executableobject.Policy, error) {
	root := executableobject.Policy{MaxBytes: maxPeerExecutableBytes, RequireRegular: true,
		RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022}
	if m.Schema != ManifestSchemaSystemd || role != RoleSSHProof {
		return root, nil
	}
	var selected *ClientManifest
	for i := range m.Clients {
		for _, candidate := range m.Clients[i].Roles {
			if candidate != role {
				continue
			}
			if selected != nil {
				return root, errors.New("native SSH proof executable authority is ambiguous")
			}
			selected = &m.Clients[i]
		}
	}
	if selected == nil {
		return root, errors.New("native SSH proof executable authority is absent")
	}
	return SystemdClientExecutablePolicy(*selected)
}
