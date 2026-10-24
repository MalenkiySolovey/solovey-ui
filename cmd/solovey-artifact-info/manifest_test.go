package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func packagingManifest(t *testing.T) release.Manifest {
	t.Helper()
	manifest := release.Manifest{Schema: release.SchemaV2, ReleaseID: "packaging-fixture", Sequence: 10,
		Version: "2026.4.1", Channel: release.ChannelMain, IssuedAt: 1_800_000_000, ExpiresAt: 1_800_000_100,
		DeploymentRevision: strings.Repeat("a", 64), MinimumPanelVersion: "2026.4.1", MaximumPanelVersion: "2026.4.1",
		MinimumCoreSchema: "1.13", MaximumCoreSchema: "1.13", TargetCoreSchema: "1.13", BrokerCapability: "broker-capabilities-1.3",
		MigrationSetDigest: strings.Repeat("b", 64), ReleaseNotesDigest: strings.Repeat("c", 64),
		RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic", Components: []release.Component{},
		CoreProviders: []release.CoreProviderSource{providerfixture.Identity(t, "sing-box").Source}}
	for _, profile := range []string{"full", "core"} {
		manifest.Artifacts = append(manifest.Artifacts, release.Artifact{Name: "fixture-" + profile + ".tar.gz",
			Role: "panel-" + profile, Platform: "linux", Arch: "amd64", MediaType: "application/gzip", Size: 1,
			SHA256: strings.Repeat("d", 64), Provenance: "packaging-test", CoreProvider: "sing-box"})
	}
	return manifest
}

func TestPackagingCanonicalManifestUsesRuntimeContract(t *testing.T) {
	manifest := packagingManifest(t)
	for _, schema := range []string{release.SchemaV1, release.SchemaV2} {
		manifest.Schema = schema
		if schema == release.SchemaV1 {
			manifest.CoreProviders = nil
			for i := range manifest.Artifacts {
				manifest.Artifacts[i].CoreProvider = ""
			}
		} else {
			manifest = packagingManifest(t)
		}
		raw, _ := json.Marshal(manifest)
		want, err := release.Canonical(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := run([]string{"--canonical-manifest"}, bytes.NewReader(raw), &output); err != nil || !bytes.Equal(output.Bytes(), want) {
			t.Fatal("packaging canonical form differs from runtime contract", err)
		}
	}
	for _, raw := range []string{`{"schema":"x","schema":"y"}`, `{"unknown":"credential-sentinel"}`, `{}`, strings.Repeat(" ", release.MaxManifestBytes+1)} {
		var output bytes.Buffer
		if err := run([]string{"--canonical-manifest"}, strings.NewReader(raw), &output); err == nil || strings.Contains(err.Error(), "credential-sentinel") || output.Len() != 0 {
			t.Fatal("invalid packaging manifest accepted or echoed")
		}
	}
}

func TestPackagingReleaseObservationUsesRuntimeSignatureAndStrictJSON(t *testing.T) {
	manifest := packagingManifest(t)
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	raw, err := release.Sign(manifest, "packaging-fixture", private)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "release.json")
	request := map[string]any{"now": manifest.IssuedAt, "channel": "main", "trustRoots": []packagingTrustRoot{{
		KeyID: "packaging-fixture", PublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)),
		State: release.RootActive, NotBefore: manifest.IssuedAt - 1, NotAfter: manifest.ExpiresAt + 1, MinSequence: 1}}}
	input, _ := json.Marshal(request)
	for name, candidate := range map[string][]byte{
		"valid":     raw,
		"duplicate": bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"credential-sentinel","schema":`), 1),
		"unknown":   bytes.Replace(raw, []byte(`"keyId":`), []byte(`"credential-sentinel":true,"keyId":`), 1),
		"signature": bytes.Replace(raw, []byte(`"signature":"`), []byte(`"signature":"AAAA`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(file, candidate, 0o600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := run([]string{"--verify-release", file}, bytes.NewReader(input), &output)
			if name != "valid" {
				if err == nil || strings.Contains(err.Error(), "credential-sentinel") || output.Len() != 0 {
					t.Fatal("invalid release observation accepted or echoed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var observation struct {
				Schema string `json:"schema"`
				Digest string `json:"digest"`
			}
			if json.Unmarshal(output.Bytes(), &observation) != nil || observation.Schema != "solovey.release-observation/v1" || len(observation.Digest) != 64 {
				t.Fatal("verified release observation is incomplete")
			}
		})
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	request["now"] = time.Unix(manifest.ExpiresAt, 0).Unix()
	expired, _ := json.Marshal(request)
	var output bytes.Buffer
	if err := run([]string{"--verify-release", file}, bytes.NewReader(expired), &output); err == nil || output.Len() != 0 {
		t.Fatal("expired release observation was accepted")
	}
	for _, invalid := range []string{`{}`, `{"now":1,"now":2}`, strings.Repeat(" ", (64<<10)+1)} {
		if err := run([]string{"--verify-release", file}, strings.NewReader(invalid), &output); err == nil || output.Len() != 0 {
			t.Fatal("invalid trust request was accepted")
		}
	}
}
