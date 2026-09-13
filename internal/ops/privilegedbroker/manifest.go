package privilegedbroker

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	ManifestSchemaSystemd       = 1
	ManifestSchemaProcd         = 2
	ProcdRelationMain           = "main-pid"
	ProcdRelationAncestor       = "ancestor"
	ProcdSelectorUniqueAncestor = "unique-command-ancestor"
	CgroupAuthorityRevisionV1   = "8ab16df455a0b11dd9f05baa6819cf6c80414df10c1eff881d0cfc30b2491f68"
)

type CgroupPolicy string

const (
	CgroupRequired CgroupPolicy = "required"
	CgroupOptional CgroupPolicy = "optional"
)

func (p CgroupPolicy) Valid() bool { return p == CgroupRequired || p == CgroupOptional }

type SupervisorProofKind string

const (
	SupervisorProofSystemd SupervisorProofKind = "systemd-cgroup"
	SupervisorProofProcd   SupervisorProofKind = "procd-service"
)

type ClientManifest struct {
	Name                    string       `json:"name"`
	UID                     uint32       `json:"uid"`
	AnyNonRootUID           bool         `json:"anyNonRootUid,omitempty"`
	GID                     uint32       `json:"gid"`
	AnyGID                  bool         `json:"anyGid,omitempty"`
	RequiredGroup           uint32       `json:"requiredGroup,omitempty"`
	Executable              string       `json:"executable"`
	ExecutableDigest        string       `json:"executableDigest"`
	Device                  uint64       `json:"device"`
	Inode                   uint64       `json:"inode"`
	CgroupUnit              string       `json:"cgroupUnit,omitempty"`
	CgroupPolicy            CgroupPolicy `json:"cgroupPolicy"`
	CgroupAuthorityRevision string       `json:"cgroupAuthorityRevision"`
	ProcdService            string       `json:"procdService,omitempty"`
	ProcdInstance           string       `json:"procdInstance,omitempty"`
	ProcdInstanceSelector   string       `json:"procdInstanceSelector,omitempty"`
	ProcdCommand            []string     `json:"procdCommand,omitempty"`
	ProcdUser               string       `json:"procdUser,omitempty"`
	ProcdGroup              string       `json:"procdGroup,omitempty"`
	ProcdRelation           string       `json:"procdRelation,omitempty"`
	ProcdExecutable         string       `json:"procdExecutable,omitempty"`
	ProcdExecutableDigest   string       `json:"procdExecutableDigest,omitempty"`
	ProcdDevice             uint64       `json:"procdDevice,omitempty"`
	ProcdInode              uint64       `json:"procdInode,omitempty"`
	CapabilitiesOnly        bool         `json:"capabilitiesOnly,omitempty"`
	Roles                   []Role       `json:"roles"`
}

type Manifest struct {
	Schema                   int              `json:"schema"`
	ApplicationOwnerRevision string           `json:"applicationOwnerRevision,omitempty"`
	Clients                  []ClientManifest `json:"clients"`
	Revision                 string           `json:"revision"`
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
	data, err := readTrustedManifest(path)
	if err != nil {
		return Manifest{}, errors.New("broker client manifest is unsafe")
	}
	var manifest Manifest
	if err := decodeStrict(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode broker client manifest: %w", err)
	}
	expected := manifest.Revision
	manifest.Revision = ""
	canonical, _ := json.Marshal(manifest)
	manifest.Revision = expected
	if manifest.Schema != ManifestSchemaSystemd && manifest.Schema != ManifestSchemaProcd || !digestPattern.MatchString(expected) || Digest(canonical) != expected || len(manifest.Clients) == 0 || len(manifest.Clients) > 16 {
		return Manifest{}, errors.New("broker client manifest revision is invalid")
	}
	seen := map[string]bool{}
	if manifest.Schema == ManifestSchemaSystemd && manifest.ApplicationOwnerRevision != "" ||
		manifest.Schema == ManifestSchemaProcd && !digestPattern.MatchString(manifest.ApplicationOwnerRevision) {
		return Manifest{}, errors.New("broker client manifest application owner revision is invalid")
	}
	for _, client := range manifest.Clients {
		exactLegacyRoot := client.UID == 0 && client.GID == 0 && !client.AnyNonRootUID && !client.AnyGID && client.CgroupUnit != "" && len(client.Roles) == 1 && client.Roles[0] == RolePanel
		if !safeIdentifier(client.Name) || seen[client.Name] || !filepath.IsAbs(client.Executable) ||
			!digestPattern.MatchString(client.ExecutableDigest) || client.Device == 0 || client.Inode == 0 ||
			!client.CgroupPolicy.Valid() || client.CgroupAuthorityRevision != CgroupAuthorityRevisionV1 ||
			client.GID == 0 && !client.AnyGID && !exactLegacyRoot || len(client.Roles) == 0 || len(client.Roles) > 2 || client.AnyNonRootUID && client.UID != 0 ||
			client.AnyGID && client.RequiredGroup == 0 {
			return Manifest{}, errors.New("broker client manifest entry is invalid")
		}
		if manifest.Schema == ManifestSchemaSystemd {
			if client.CgroupPolicy != CgroupRequired {
				return Manifest{}, errors.New("systemd broker client cgroup policy must be required")
			}
			if client.ProcdService != "" || client.ProcdInstance != "" || client.ProcdInstanceSelector != "" || len(client.ProcdCommand) != 0 || client.ProcdUser != "" || client.ProcdGroup != "" || client.ProcdRelation != "" ||
				client.ProcdExecutable != "" || client.ProcdExecutableDigest != "" || client.ProcdDevice != 0 || client.ProcdInode != 0 {
				return Manifest{}, errors.New("systemd broker client manifest contains procd identity")
			}
		} else if err := validateProcdManifestIdentity(client); err != nil {
			return Manifest{}, err
		}
		seen[client.Name] = true
		for _, role := range client.Roles {
			if role != RolePanel && role != RoleSSHProof || (client.AnyNonRootUID || client.AnyGID) && role != RoleSSHProof {
				return Manifest{}, errors.New("broker client manifest role is invalid")
			}
		}
		if client.CapabilitiesOnly && (len(client.Roles) != 1 || client.Roles[0] != RolePanel) {
			return Manifest{}, errors.New("capability-only broker client role is invalid")
		}
	}
	return manifest, nil
}

func validateProcdManifestIdentity(client ClientManifest) error {
	instanceValid := client.ProcdRelation == ProcdRelationMain && safeIdentifier(client.ProcdInstance) && client.ProcdInstanceSelector == "" ||
		client.ProcdRelation == ProcdRelationAncestor && client.ProcdInstance == "" && client.ProcdInstanceSelector == ProcdSelectorUniqueAncestor
	if client.CgroupUnit != "" || !safeIdentifier(client.ProcdService) || !instanceValid ||
		!safeIdentifier(client.ProcdUser) || !safeIdentifier(client.ProcdGroup) ||
		client.ProcdRelation != ProcdRelationMain && client.ProcdRelation != ProcdRelationAncestor ||
		len(client.ProcdCommand) == 0 || len(client.ProcdCommand) > 16 {
		return errors.New("procd broker client manifest identity is invalid")
	}
	for index, argument := range client.ProcdCommand {
		if argument == "" || len(argument) > 512 || strings.ContainsAny(argument, "\x00\r\n") || index == 0 && !filepath.IsAbs(argument) {
			return errors.New("procd broker client manifest command is invalid")
		}
	}
	if client.ProcdRelation == ProcdRelationMain {
		if client.ProcdExecutable != "" || client.ProcdExecutableDigest != "" || client.ProcdDevice != 0 || client.ProcdInode != 0 {
			return errors.New("procd main process identity has unexpected ancestor fields")
		}
		if len(client.Roles) != 1 || client.Roles[0] != RolePanel {
			return errors.New("procd main process identity is restricted to the panel role")
		}
	} else {
		if len(client.Roles) != 1 || client.Roles[0] != RoleSSHProof || client.CapabilitiesOnly ||
			client.ProcdExecutable != client.ProcdCommand[0] || !filepath.IsAbs(client.ProcdExecutable) ||
			!digestPattern.MatchString(client.ProcdExecutableDigest) || client.ProcdDevice == 0 || client.ProcdInode == 0 {
			return errors.New("procd ancestor identity requires an authenticated SSH proof proposition")
		}
	}
	return nil
}

func (m Manifest) RequiresProcd() bool { return m.Schema == ManifestSchemaProcd }

// SupervisorProof identifies the concrete peer-attestation adapter encoded by
// the released manifest schema. It is intentionally independent of the socket
// transport selected by the deployment entrypoint.
func (m Manifest) SupervisorProof() (SupervisorProofKind, error) {
	switch m.Schema {
	case ManifestSchemaSystemd:
		return SupervisorProofSystemd, nil
	case ManifestSchemaProcd:
		return SupervisorProofProcd, nil
	default:
		return "", errors.New("broker manifest supervisor proof is unsupported")
	}
}

func (m Manifest) matching(role Role, identity PeerIdentity) (ClientManifest, bool) {
	for _, client := range m.commonMatching(role, identity) {
		procdInstanceMatches := client.ProcdInstance == identity.ProcdInstance || client.ProcdRelation == ProcdRelationAncestor &&
			client.ProcdInstanceSelector == ProcdSelectorUniqueAncestor && safeIdentifier(identity.ProcdInstance)
		if (client.CgroupUnit == "" || client.CgroupUnit == identity.CgroupUnit) &&
			(client.ProcdService == "" || identity.Supervisor == "procd" && client.ProcdService == identity.ProcdService &&
				procdInstanceMatches && client.ProcdRelation == identity.SupervisorRelation) {
			return client, true
		}
	}
	return ClientManifest{}, false
}

func (m Manifest) commonMismatchClass(role Role, identity PeerIdentity) PeerAttestationClass {
	roleClients := make([]ClientManifest, 0, len(m.Clients))
	for _, client := range m.Clients {
		for _, allowed := range client.Roles {
			if allowed == role {
				roleClients = append(roleClients, client)
				break
			}
		}
	}
	if len(roleClients) == 0 {
		return PeerAttestationRoleNotAuthorized
	}
	accountMatches := make([]ClientManifest, 0, len(roleClients))
	for _, client := range roleClients {
		uidMatches := client.UID == identity.UID || client.AnyNonRootUID && identity.UID != 0
		gidMatches := client.GID == identity.GID || client.AnyGID && (client.RequiredGroup == identity.GID || containsGroup(identity.Groups, client.RequiredGroup))
		if uidMatches && gidMatches {
			accountMatches = append(accountMatches, client)
		}
	}
	if len(accountMatches) == 0 {
		return PeerAttestationUIDGIDMismatch
	}
	for _, client := range accountMatches {
		if client.ExecutableDigest == identity.ExecutableDigest &&
			client.Device == identity.Device && client.Inode == identity.Inode {
			return PeerAttestationManifestAmbiguous
		}
	}
	return PeerAttestationExecutableMismatch
}

func (m Manifest) commonMatching(role Role, identity PeerIdentity) []ClientManifest {
	result := make([]ClientManifest, 0, 1)
	for _, client := range m.Clients {
		roleAllowed := false
		for _, candidate := range client.Roles {
			roleAllowed = roleAllowed || candidate == role
		}
		uidMatches := client.UID == identity.UID || client.AnyNonRootUID && identity.UID != 0
		gidMatches := client.GID == identity.GID || client.AnyGID && (client.RequiredGroup == identity.GID || containsGroup(identity.Groups, client.RequiredGroup))
		if roleAllowed && uidMatches && gidMatches &&
			client.ExecutableDigest == identity.ExecutableDigest && client.Device == identity.Device && client.Inode == identity.Inode {
			result = append(result, client)
		}
	}
	return result
}

func containsGroup(values []uint32, expected uint32) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
