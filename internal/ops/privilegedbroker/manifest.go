package privilegedbroker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type ClientManifest struct {
	Name             string `json:"name"`
	UID              uint32 `json:"uid"`
	AnyNonRootUID    bool   `json:"anyNonRootUid,omitempty"`
	GID              uint32 `json:"gid"`
	AnyGID           bool   `json:"anyGid,omitempty"`
	RequiredGroup    uint32 `json:"requiredGroup,omitempty"`
	Executable       string `json:"executable"`
	ExecutableDigest string `json:"executableDigest"`
	Device           uint64 `json:"device"`
	Inode            uint64 `json:"inode"`
	CgroupUnit       string `json:"cgroupUnit,omitempty"`
	Roles            []Role `json:"roles"`
}

type Manifest struct {
	Schema   int              `json:"schema"`
	Clients  []ClientManifest `json:"clients"`
	Revision string           `json:"revision"`
}

// FinalizeManifest returns the canonical digest-bound representation used by
// both the installer writer and the broker reader.
func FinalizeManifest(manifest Manifest) (Manifest, error) {
	manifest.Revision = ""
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Revision = Digest(canonical)
	return manifest, nil
}

func LoadManifest(path string) (Manifest, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || filepath.Base(path) != "broker-clients.json" {
		return Manifest{}, errors.New("broker client manifest path is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 256<<10 {
		return Manifest{}, errors.New("broker client manifest is unsafe")
	}
	if err := validateOwnedByRoot(path); err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := decodeStrict(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode broker client manifest: %w", err)
	}
	expected := manifest.Revision
	manifest.Revision = ""
	canonical, _ := json.Marshal(manifest)
	manifest.Revision = expected
	if manifest.Schema != 1 || !digestPattern.MatchString(expected) || Digest(canonical) != expected || len(manifest.Clients) == 0 || len(manifest.Clients) > 16 {
		return Manifest{}, errors.New("broker client manifest revision is invalid")
	}
	seen := map[string]bool{}
	for _, client := range manifest.Clients {
		exactLegacyRoot := client.UID == 0 && client.GID == 0 && !client.AnyNonRootUID && !client.AnyGID && client.CgroupUnit != "" && len(client.Roles) == 1 && client.Roles[0] == RolePanel
		if !safeIdentifier(client.Name) || seen[client.Name] || !filepath.IsAbs(client.Executable) ||
			!digestPattern.MatchString(client.ExecutableDigest) || client.Device == 0 || client.Inode == 0 ||
			client.GID == 0 && !client.AnyGID && !exactLegacyRoot || len(client.Roles) == 0 || len(client.Roles) > 2 || client.AnyNonRootUID && client.UID != 0 ||
			client.AnyGID && client.RequiredGroup == 0 {
			return Manifest{}, errors.New("broker client manifest entry is invalid")
		}
		seen[client.Name] = true
		for _, role := range client.Roles {
			if role != RolePanel && role != RoleSSHProof || (client.AnyNonRootUID || client.AnyGID) && role != RoleSSHProof {
				return Manifest{}, errors.New("broker client manifest role is invalid")
			}
		}
	}
	return manifest, nil
}

func (m Manifest) matching(role Role, identity PeerIdentity) (ClientManifest, bool) {
	for _, client := range m.Clients {
		roleAllowed := false
		for _, candidate := range client.Roles {
			roleAllowed = roleAllowed || candidate == role
		}
		uidMatches := client.UID == identity.UID || client.AnyNonRootUID && identity.UID != 0
		gidMatches := client.GID == identity.GID || client.AnyGID && (client.RequiredGroup == identity.GID || containsGroup(identity.Groups, client.RequiredGroup))
		if roleAllowed && uidMatches && gidMatches && client.Executable == identity.Executable &&
			client.ExecutableDigest == identity.ExecutableDigest && client.Device == identity.Device && client.Inode == identity.Inode &&
			(client.CgroupUnit == "" || client.CgroupUnit == identity.CgroupUnit) {
			return client, true
		}
	}
	return ClientManifest{}, false
}

func containsGroup(values []uint32, expected uint32) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
