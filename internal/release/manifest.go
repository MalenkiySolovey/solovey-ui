// Package release owns the deterministic, signed release-set contract used by
// the panel, release tooling, installer and privileged update broker.
package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	SchemaV1           = "solovey.release/v1"
	SignatureAlgorithm = "Ed25519"
	MaxManifestBytes   = 2 << 20
	MaxArtifacts       = 128
	MaxComponents      = 256
	MaxArtifactBytes   = int64(1 << 30)
	MaxReleaseSetBytes = int64(2 << 30)
)

type Channel string

const (
	ChannelMain Channel = "main"
	ChannelBeta Channel = "beta"
)

type Artifact struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
	MediaType  string `json:"mediaType"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Provenance string `json:"provenance"`
}

type Component struct {
	ID                string `json:"id"`
	Version           string `json:"version"`
	ArtifactSHA256    string `json:"artifactSha256"`
	MinimumCoreSchema string `json:"minimumCoreSchema"`
	MaximumCoreSchema string `json:"maximumCoreSchema"`
}

type Manifest struct {
	Schema              string      `json:"schema"`
	ReleaseID           string      `json:"releaseId"`
	Sequence            uint64      `json:"sequence"`
	Version             string      `json:"version"`
	Channel             Channel     `json:"channel"`
	IssuedAt            int64       `json:"issuedAt"`
	ExpiresAt           int64       `json:"expiresAt"`
	DeploymentRevision  string      `json:"deploymentRevision"`
	MinimumPanelVersion string      `json:"minimumPanelVersion"`
	MaximumPanelVersion string      `json:"maximumPanelVersion"`
	MinimumCoreSchema   string      `json:"minimumCoreSchema"`
	MaximumCoreSchema   string      `json:"maximumCoreSchema"`
	TargetCoreSchema    string      `json:"targetCoreSchema"`
	BrokerCapability    string      `json:"brokerCapability"`
	MigrationSetDigest  string      `json:"migrationSetDigest"`
	ReleaseNotesDigest  string      `json:"releaseNotesDigest"`
	RestartClass        string      `json:"restartClass"`
	RebootClass         string      `json:"rebootClass"`
	RollbackClass       string      `json:"rollbackClass"`
	Artifacts           []Artifact  `json:"artifacts"`
	Components          []Component `json:"components"`
}

type Envelope struct {
	Schema    string          `json:"schema"`
	KeyID     string          `json:"keyId"`
	Algorithm string          `json:"algorithm"`
	Manifest  json.RawMessage `json:"manifest"`
	Signature string          `json:"signature"`
}

type RootState string

const (
	RootActive  RootState = "ACTIVE"
	RootNext    RootState = "NEXT"
	RootRetired RootState = "RETIRED"
)

type TrustRoot struct {
	KeyID       string
	PublicKey   ed25519.PublicKey
	State       RootState
	NotBefore   time.Time
	NotAfter    time.Time
	MinSequence uint64
	MaxSequence uint64
}

type TrustStore struct{ roots map[string]TrustRoot }

type Verified struct {
	Manifest  Manifest
	Canonical []byte
	Digest    string
	KeyID     string
}

var (
	identifier   = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,95}$`)
	digest       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	version      = regexp.MustCompile(`^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}(?:-[0-9A-Za-z.-]{1,64})?$`)
	artifactName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$`)
	mediaType    = regexp.MustCompile(`^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+$`)
)

// Each panel archive is a coherent release set: the broker verifies its
// panel, broker, proof, manifest-writer, manager and systemd members before
// activation. Both binary profiles must be present for every Linux target.
var requiredNativeRoles = []string{"panel-full", "panel-core"}

func NewTrustStore(roots []TrustRoot) (TrustStore, error) {
	store := TrustStore{roots: make(map[string]TrustRoot, len(roots))}
	for _, root := range roots {
		if !identifier.MatchString(root.KeyID) || len(root.PublicKey) != ed25519.PublicKeySize ||
			(root.State != RootActive && root.State != RootNext && root.State != RootRetired) ||
			root.NotBefore.IsZero() || root.NotAfter.IsZero() || !root.NotAfter.After(root.NotBefore) ||
			root.MinSequence == 0 || (root.MaxSequence != 0 && root.MaxSequence < root.MinSequence) {
			return TrustStore{}, errors.New("invalid release trust root")
		}
		if _, exists := store.roots[root.KeyID]; exists {
			return TrustStore{}, fmt.Errorf("duplicate release trust root %q", root.KeyID)
		}
		copyRoot := root
		copyRoot.PublicKey = append(ed25519.PublicKey(nil), root.PublicKey...)
		store.roots[root.KeyID] = copyRoot
	}
	return store, nil
}

func (s TrustStore) Available(now time.Time) bool {
	for _, root := range s.roots {
		if (root.State == RootActive || root.State == RootNext) && !now.Before(root.NotBefore) && now.Before(root.NotAfter) {
			return true
		}
	}
	return false
}

func Canonical(manifest Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	clone := manifest
	clone.Artifacts = append([]Artifact(nil), manifest.Artifacts...)
	clone.Components = append([]Component(nil), manifest.Components...)
	sort.Slice(clone.Artifacts, func(i, j int) bool {
		left, right := clone.Artifacts[i], clone.Artifacts[j]
		return left.Platform+"\x00"+left.Arch+"\x00"+left.Role+"\x00"+left.Name < right.Platform+"\x00"+right.Arch+"\x00"+right.Role+"\x00"+right.Name
	})
	sort.Slice(clone.Components, func(i, j int) bool { return clone.Components[i].ID < clone.Components[j].ID })
	return json.Marshal(clone)
}

func Sign(manifest Manifest, keyID string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if !identifier.MatchString(keyID) || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid release signing identity")
	}
	canonical, err := Canonical(manifest)
	if err != nil {
		return nil, err
	}
	envelope := Envelope{Schema: SchemaV1, KeyID: keyID, Algorithm: SignatureAlgorithm,
		Manifest: canonical, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical))}
	return json.Marshal(envelope)
}

func Verify(raw []byte, store TrustStore, now time.Time, channel Channel, lastSequence uint64) (Verified, error) {
	if len(raw) == 0 || len(raw) > MaxManifestBytes {
		return Verified{}, errors.New("release envelope size is invalid")
	}
	var envelope Envelope
	if err := decodeStrict(raw, &envelope); err != nil {
		return Verified{}, fmt.Errorf("decode release envelope: %w", err)
	}
	if envelope.Schema != SchemaV1 || envelope.Algorithm != SignatureAlgorithm || !identifier.MatchString(envelope.KeyID) {
		return Verified{}, errors.New("release envelope identity is invalid")
	}
	root, ok := store.roots[envelope.KeyID]
	if !ok || root.State == RootRetired || now.Before(root.NotBefore) || !now.Before(root.NotAfter) {
		return Verified{}, errors.New("release signing root is unavailable")
	}
	var manifest Manifest
	if err := decodeStrict(envelope.Manifest, &manifest); err != nil {
		return Verified{}, fmt.Errorf("decode release manifest: %w", err)
	}
	canonical, err := Canonical(manifest)
	if err != nil {
		return Verified{}, err
	}
	if !bytes.Equal(canonical, envelope.Manifest) {
		return Verified{}, errors.New("release manifest is not in canonical form")
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(root.PublicKey, canonical, signature) {
		return Verified{}, errors.New("release signature is invalid")
	}
	if manifest.Channel != channel || manifest.Sequence <= lastSequence || manifest.Sequence < root.MinSequence ||
		(root.MaxSequence != 0 && manifest.Sequence > root.MaxSequence) {
		return Verified{}, errors.New("release sequence or channel is not acceptable")
	}
	if now.Unix() < manifest.IssuedAt || now.Unix() >= manifest.ExpiresAt {
		return Verified{}, errors.New("release manifest is not currently valid")
	}
	sum := sha256.Sum256(canonical)
	return Verified{Manifest: manifest, Canonical: canonical, Digest: hex.EncodeToString(sum[:]), KeyID: envelope.KeyID}, nil
}

func (m Manifest) Validate() error {
	if m.Schema != SchemaV1 || !identifier.MatchString(m.ReleaseID) || m.Sequence == 0 || !version.MatchString(m.Version) ||
		(m.Channel != ChannelMain && m.Channel != ChannelBeta) || m.IssuedAt <= 0 || m.ExpiresAt <= m.IssuedAt ||
		m.ExpiresAt-m.IssuedAt > int64((14*24*time.Hour)/time.Second) || !digest.MatchString(m.DeploymentRevision) ||
		!version.MatchString(m.MinimumPanelVersion) || !version.MatchString(m.MaximumPanelVersion) ||
		!schemaVersion(m.MinimumCoreSchema) || !schemaVersion(m.MaximumCoreSchema) || !schemaVersion(m.TargetCoreSchema) ||
		!identifier.MatchString(m.BrokerCapability) || !digest.MatchString(m.MigrationSetDigest) ||
		!digest.MatchString(m.ReleaseNotesDigest) || !oneOf(m.RestartClass, "panel", "stack") ||
		!oneOf(m.RebootClass, "not-required", "operator-advisory") || !oneOf(m.RollbackClass, "automatic", "manual-recovery") {
		return errors.New("release manifest header is invalid")
	}
	if compareSemanticVersions(m.MinimumPanelVersion, m.MaximumPanelVersion) > 0 ||
		compareSchemaVersions(m.MinimumCoreSchema, m.MaximumCoreSchema) > 0 ||
		compareSchemaVersions(m.TargetCoreSchema, m.MinimumCoreSchema) < 0 ||
		compareSchemaVersions(m.TargetCoreSchema, m.MaximumCoreSchema) > 0 {
		return errors.New("release manifest compatibility range is invalid")
	}
	if len(m.Artifacts) == 0 || len(m.Artifacts) > MaxArtifacts || len(m.Components) > MaxComponents {
		return errors.New("release manifest cardinality is invalid")
	}
	seenArtifacts := map[string]struct{}{}
	seenRoles := map[string]struct{}{}
	artifactDigests := map[string]struct{}{}
	rolesByTarget := map[string]map[string]struct{}{}
	totalBytes := int64(0)
	for _, artifact := range m.Artifacts {
		if !artifactName.MatchString(artifact.Name) || !identifier.MatchString(artifact.Role) ||
			!identifier.MatchString(artifact.Platform) || !identifier.MatchString(artifact.Arch) ||
			!mediaType.MatchString(artifact.MediaType) || len(artifact.MediaType) > 96 || artifact.Size <= 0 || artifact.Size > MaxArtifactBytes ||
			!digest.MatchString(artifact.SHA256) || !identifier.MatchString(artifact.Provenance) {
			return errors.New("release artifact is invalid")
		}
		if totalBytes > MaxReleaseSetBytes-artifact.Size {
			return errors.New("release artifact set exceeds total size limit")
		}
		totalBytes += artifact.Size
		identity := artifact.Platform + "\x00" + artifact.Arch + "\x00" + artifact.Role + "\x00" + artifact.Name
		if _, exists := seenArtifacts[identity]; exists {
			return errors.New("release artifact identity is duplicated")
		}
		seenArtifacts[identity] = struct{}{}
		roleIdentity := artifact.Platform + "\x00" + artifact.Arch + "\x00" + artifact.Role
		if _, exists := seenRoles[roleIdentity]; exists {
			return errors.New("release artifact role is duplicated")
		}
		seenRoles[roleIdentity] = struct{}{}
		artifactDigests[artifact.SHA256] = struct{}{}
		target := artifact.Platform + "/" + artifact.Arch
		if rolesByTarget[target] == nil {
			rolesByTarget[target] = map[string]struct{}{}
		}
		rolesByTarget[target][artifact.Role] = struct{}{}
	}
	for target, roles := range rolesByTarget {
		if strings.HasPrefix(target, "linux/") {
			for _, role := range requiredNativeRoles {
				if _, ok := roles[role]; !ok {
					return fmt.Errorf("release target %s lacks coherent role %s", target, role)
				}
			}
		}
	}
	seenComponents := map[string]struct{}{}
	for _, component := range m.Components {
		if !identifier.MatchString(component.ID) || !version.MatchString(component.Version) ||
			!digest.MatchString(component.ArtifactSHA256) || !schemaVersion(component.MinimumCoreSchema) ||
			!schemaVersion(component.MaximumCoreSchema) ||
			compareSchemaVersions(component.MinimumCoreSchema, component.MaximumCoreSchema) > 0 {
			return errors.New("release component is invalid")
		}
		if _, exists := seenComponents[component.ID]; exists {
			return errors.New("release component identity is duplicated")
		}
		if _, exists := artifactDigests[component.ArtifactSHA256]; !exists {
			return errors.New("release component artifact is outside the coherent set")
		}
		seenComponents[component.ID] = struct{}{}
	}
	return nil
}

func (m Manifest) ArtifactsFor(platform, arch, profile string) ([]Artifact, error) {
	if profile != "full" && profile != "core" {
		return nil, errors.New("release binary profile is unsupported")
	}
	wantedRole := "panel-" + profile
	selected := make([]Artifact, 0, 1)
	for _, artifact := range m.Artifacts {
		if artifact.Platform == platform && artifact.Arch == arch && artifact.Role == wantedRole {
			selected = append(selected, artifact)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("release does not support this platform")
	}
	if len(selected) != 1 {
		return nil, errors.New("release profile artifact set is ambiguous")
	}
	return selected, nil
}

func ArtifactSetDigest(artifacts []Artifact) string {
	copyArtifacts := append([]Artifact(nil), artifacts...)
	sort.Slice(copyArtifacts, func(i, j int) bool {
		return copyArtifacts[i].Role+"\x00"+copyArtifacts[i].Name < copyArtifacts[j].Role+"\x00"+copyArtifacts[j].Name
	})
	data, _ := json.Marshal(copyArtifacts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}

func schemaVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func compareSemanticVersions(left, right string) int {
	leftCore, leftPre, _ := strings.Cut(left, "-")
	rightCore, rightPre, _ := strings.Cut(right, "-")
	leftParts, rightParts := strings.Split(leftCore, "."), strings.Split(rightCore, ".")
	for index := 0; index < 3; index++ {
		leftValue, rightValue := trimNumeric(leftParts[index]), trimNumeric(rightParts[index])
		if comparison := compareNumericText(leftValue, rightValue); comparison != 0 {
			return comparison
		}
	}
	if leftPre == "" || rightPre == "" {
		switch {
		case leftPre == rightPre:
			return 0
		case leftPre == "":
			return 1
		default:
			return -1
		}
	}
	leftIdentifiers, rightIdentifiers := strings.Split(leftPre, "."), strings.Split(rightPre, ".")
	for index := 0; index < len(leftIdentifiers) || index < len(rightIdentifiers); index++ {
		if index >= len(leftIdentifiers) {
			return -1
		}
		if index >= len(rightIdentifiers) {
			return 1
		}
		leftIdentifier, rightIdentifier := leftIdentifiers[index], rightIdentifiers[index]
		if leftIdentifier == rightIdentifier {
			continue
		}
		leftNumeric, rightNumeric := allDigits(leftIdentifier), allDigits(rightIdentifier)
		switch {
		case leftNumeric && rightNumeric:
			return compareNumericText(trimNumeric(leftIdentifier), trimNumeric(rightIdentifier))
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case leftIdentifier < rightIdentifier:
			return -1
		default:
			return 1
		}
	}
	return 0
}

func compareSchemaVersions(left, right string) int {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < 2; index++ {
		if comparison := compareNumericText(trimNumeric(leftParts[index]), trimNumeric(rightParts[index])); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func compareNumericText(left, right string) int {
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func trimNumeric(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
