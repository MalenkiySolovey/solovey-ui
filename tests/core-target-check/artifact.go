package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// Ephemeral qualification signatures exercise the existing trust owner against
// an actual built executable. They are fixtures, never release artifacts.
func inspectArtifactFixture(file *os.File, source coreprovider.Source, contentDigest string, size int64) ([]string, error) {
	observed, err := release.InspectPanelBuild(file, source)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	manifest := release.Manifest{Schema: release.SchemaV2, ReleaseID: "qualification-fixture", Sequence: 1,
		Version: "0.0.0", Channel: release.ChannelMain, IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
		DeploymentRevision: strings.Repeat("0", 64), MinimumPanelVersion: "0.0.0", MaximumPanelVersion: "0.0.0",
		MinimumCoreSchema: "1.13", MaximumCoreSchema: "1.13", TargetCoreSchema: "1.13", BrokerCapability: "fixture",
		MigrationSetDigest: strings.Repeat("0", 64), ReleaseNotesDigest: strings.Repeat("0", 64),
		RestartClass: "stack", RebootClass: "not-required", RollbackClass: "automatic", CoreProviders: []release.CoreProviderSource{source},
		Artifacts: []release.Artifact{}, Components: []release.Component{}}
	for _, profile := range []string{"full", "core"} {
		manifest.Artifacts = append(manifest.Artifacts, release.Artifact{Name: "qualification-" + profile, Role: "panel-" + profile,
			Platform: observed.Platform, Arch: observed.Arch, MediaType: "application/octet-stream", Size: size,
			SHA256: contentDigest, Provenance: "qualification-fixture", CoreProvider: source.ID})
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	store, err := release.NewTrustStore([]release.TrustRoot{{KeyID: "qualification-fixture", PublicKey: public, State: release.RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		return nil, err
	}
	verify := func(m release.Manifest) (release.Verified, error) {
		raw, err := release.Sign(m, "qualification-fixture", private)
		if err != nil {
			return release.Verified{}, err
		}
		return release.Verify(raw, store, now, release.ChannelMain, 0)
	}
	verified, err := verify(manifest)
	if err != nil {
		return nil, err
	}
	selected, err := verified.PanelArtifactsForProvider(observed.Platform, observed.Arch, observed.BinaryProfile, source.ID)
	if err != nil || len(selected) != 1 {
		return nil, errors.New("signed fixture selection failed")
	}
	signedObservation, err := verified.InspectPanelArtifact(file, selected[0])
	if err != nil || !reflect.DeepEqual(observed, signedObservation) {
		return nil, errors.New("signed artifact differs from actual executable observation")
	}
	forged := release.Verified{Manifest: verified.Manifest, Canonical: verified.Canonical, Digest: verified.Digest, KeyID: verified.KeyID}
	if _, err := forged.InspectPanelArtifact(file, selected[0]); err == nil {
		return nil, errors.New("fixture metadata manufactured signature trust")
	}
	changed := selected[0]
	changed.SHA256 = strings.Repeat("0", 64)
	if _, err := verified.InspectPanelArtifact(file, changed); err == nil {
		return nil, errors.New("changed fixture artifact retained trust")
	}
	otherProfile := "full"
	if observed.BinaryProfile == "full" {
		otherProfile = "core"
	}
	other, err := verified.PanelArtifactsForProvider(observed.Platform, observed.Arch, otherProfile, source.ID)
	if err != nil {
		return nil, err
	}
	if _, err := verified.InspectPanelArtifact(file, other[0]); err == nil {
		return nil, errors.New("actual binary accepted conflicting signed profile")
	}
	for _, mutate := range []func(*release.Manifest){
		func(m *release.Manifest) {
			for i := range m.Artifacts {
				m.Artifacts[i].Arch = "386"
				if observed.Arch == "386" {
					m.Artifacts[i].Arch = "amd64"
				}
			}
		},
		func(m *release.Manifest) { m.CoreProviders[0].ModuleSum = "h1:" + strings.Repeat("A", 43) + "=" },
		func(m *release.Manifest) {
			id, name := "shtorm-extended", "Shtorm Extended"
			repository := "https://github.com/MalenkiySolovey/sing-box-extended"
			upstream := "https://github.com/shtorm-7/sing-box-extended"
			if source.ID == id {
				id, name = "sing-box", "Official"
				repository = "https://github.com/MalenkiySolovey/sing-box"
				upstream = "https://github.com/SagerNet/sing-box"
			}
			m.CoreProviders[0].ID, m.CoreProviders[0].Name = id, name
			m.CoreProviders[0].SourceRepository = repository
			m.CoreProviders[0].Module = strings.TrimPrefix(repository, "https://")
			m.CoreProviders[0].UpstreamRepository = upstream
			for i := range m.Artifacts {
				m.Artifacts[i].CoreProvider = id
			}
		},
	} {
		changedManifest := manifest
		changedManifest.Artifacts = append([]release.Artifact(nil), manifest.Artifacts...)
		changedManifest.CoreProviders = append([]release.CoreProviderSource(nil), manifest.CoreProviders...)
		mutate(&changedManifest)
		changedRelease, err := verify(changedManifest)
		if err != nil {
			return nil, err
		}
		for _, candidate := range changedRelease.Manifest.Artifacts {
			if candidate.Role == "panel-"+observed.BinaryProfile {
				if _, err := changedRelease.InspectPanelArtifact(file, candidate); err == nil {
					return nil, errors.New("actual binary accepted conflicting signed target, source or ABI")
				}
			}
		}
	}
	patched, err := patchMachine(file)
	if err != nil {
		return nil, err
	}
	if _, err := release.InspectPanelBuild(patched, source); err == nil {
		return nil, errors.New("actual executable machine disagreed with accepted Go metadata")
	}
	return []string{"signed-source-target-profile", "no-forged-trust", "artifact-membership", "wrong-profile", "wrong-target", "wrong-module-sum", "wrong-provider-ABI", "machine-vs-Go-settings"}, nil
}

type machinePatch struct {
	reader  io.ReaderAt
	offset  int64
	machine [2]byte
}

func (p machinePatch) ReadAt(buffer []byte, offset int64) (int, error) {
	n, err := p.reader.ReadAt(buffer, offset)
	for i := 0; i < n; i++ {
		if index := offset + int64(i) - p.offset; index >= 0 && index < 2 {
			buffer[i] = p.machine[index]
		}
	}
	return n, err
}

func patchMachine(reader io.ReaderAt) (machinePatch, error) {
	var header [64]byte
	if _, err := reader.ReadAt(header[:], 0); err != nil {
		return machinePatch{}, err
	}
	patch := machinePatch{reader: reader, offset: 18}
	if string(header[:4]) == "\x7fELF" {
		var order binary.ByteOrder = binary.LittleEndian
		if header[5] == 2 {
			order = binary.BigEndian
		}
		machine := uint16(62)
		if order.Uint16(header[18:]) == machine {
			machine = 183
		}
		order.PutUint16(patch.machine[:], machine)
	} else if string(header[:2]) == "MZ" {
		patch.offset = int64(binary.LittleEndian.Uint32(header[60:])) + 4
		var current [2]byte
		if _, err := reader.ReadAt(current[:], patch.offset); err != nil {
			return machinePatch{}, err
		}
		machine := uint16(0x8664)
		if binary.LittleEndian.Uint16(current[:]) == machine {
			machine = 0x14c
		}
		binary.LittleEndian.PutUint16(patch.machine[:], machine)
	} else {
		return machinePatch{}, errors.New("fixture executable format unavailable")
	}
	return patch, nil
}
