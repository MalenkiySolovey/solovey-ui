package release

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func TestLegacyReleaseV1CanonicalAndSignatureBytesRemainStable(t *testing.T) {
	manifest := validManifest(time.Unix(1_800_000_000, 0))
	canonical, err := Canonical(manifest)
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	signed, err := Sign(manifest, "legacy-fixture", ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	// Captured on the accepted v1 implementation before adding v2 fields.
	if digestBytes(canonical) != "dbe72895464127173244dab89e72c75de035ae8e3750a158c4e8eef0ae0ad38f" ||
		digestBytes(signed) != "2bf74e4de6077945c524a002e4ce63479ad511a0a26e5b32f8e1eedfd0f81728" {
		t.Fatal("legacy canonical or signed-envelope bytes changed")
	}
}

func providerManifest(t *testing.T, now time.Time) Manifest {
	t.Helper()
	manifest := validManifest(now)
	manifest.Schema = SchemaV2
	manifest.CoreProviders = []CoreProviderSource{
		providerfixture.Identity(t, "shtorm-extended").Source,
		providerfixture.Identity(t, "sing-box").Source,
	}
	for i := range manifest.Artifacts {
		if strings.HasPrefix(manifest.Artifacts[i].Role, "panel-") {
			manifest.Artifacts[i].CoreProvider = "sing-box"
		}
	}
	for _, artifact := range slices.Clone(manifest.Artifacts) {
		if artifact.CoreProvider == "" {
			continue
		}
		artifact.CoreProvider = "shtorm-extended"
		// The typed provider binding, never this intentionally misleading name,
		// is the selection authority.
		artifact.Name = "apparently-official-" + artifact.Role + ".tar.gz"
		artifact.SHA256 = digestFor("extended-" + artifact.Role)
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	return manifest
}

func TestProviderReleaseSelectionRequiresExplicitAuthenticatedIdentity(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	manifest := providerManifest(t, now)
	before, _ := json.Marshal(manifest)
	canonical, err := Canonical(manifest)
	if err != nil {
		t.Fatal(err)
	}
	copyManifest := manifest
	copyManifest.Artifacts, copyManifest.CoreProviders = slices.Clone(manifest.Artifacts), slices.Clone(manifest.CoreProviders)
	slices.Reverse(copyManifest.Artifacts)
	slices.Reverse(copyManifest.CoreProviders)
	reordered, err := Canonical(copyManifest)
	after, _ := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, reordered) || !bytes.Equal(before, after) {
		t.Fatal("provider canonical ordering changed source metadata or depended on input order")
	}
	verified := verifiedIdentityFixture(t, manifest, now)
	if _, err := verified.PanelArtifacts("linux", "amd64", "full"); err == nil {
		t.Fatal("v2 selected a provider implicitly")
	}
	for _, provider := range []string{"sing-box", "shtorm-extended"} {
		for _, profile := range []string{"full", "core"} {
			selected, err := verified.PanelArtifactsForProvider("linux", "amd64", profile, provider)
			if err != nil || len(selected) != 1 || selected[0].CoreProvider != provider || selected[0].Role != "panel-"+profile {
				t.Fatalf("explicit provider selection failed: %s/%s %v", provider, profile, err)
			}
			identity, err := verified.ArtifactIdentity(selected[0])
			wantSource := providerfixture.Identity(t, provider).Source
			if err != nil || identity.CoreProvider != wantSource ||
				identity.ContentDigest != selected[0].SHA256 || identity.SourceRevision != verified.Digest {
				t.Fatalf("provider/source/content binding lost: %v", err)
			}
			identical, err := verified.ArtifactIdentity(selected[0])
			if err != nil || identical != identity {
				t.Fatal("equivalent artifact projections lost value equality")
			}
			identity.CoreProvider.SourceRevision = strings.Repeat("0", 40)
			fresh, err := verified.ArtifactIdentity(selected[0])
			if err != nil || fresh.CoreProvider != wantSource {
				t.Fatal("returned provider projection mutated signed metadata")
			}
		}
	}
	for _, selection := range [][4]string{
		{"linux", "amd64", "full", ""}, {"linux", "amd64", "full", "unknown"},
		{"linux", "arm64", "full", "shtorm-extended"}, {"linux", "amd64", "other", "sing-box"},
	} {
		if _, err := verified.PanelArtifactsForProvider(selection[0], selection[1], selection[2], selection[3]); err == nil {
			t.Fatal("unsupported selection silently chose an artifact")
		}
	}
	forged := Verified{Manifest: verified.Manifest, Canonical: verified.Canonical, Digest: verified.Digest, KeyID: verified.KeyID}
	if _, err := forged.PanelArtifactsForProvider("linux", "amd64", "full", "shtorm-extended"); err == nil {
		t.Fatal("public metadata manufactured provider artifact trust")
	}
	legacy := verifiedIdentityFixture(t, validManifest(now), now)
	if _, err := legacy.PanelArtifactsForProvider("linux", "amd64", "full", "shtorm-extended"); err == nil {
		t.Fatal("legacy release authorized Extended")
	}
	selected, err := legacy.PanelArtifactsForProvider("linux", "amd64", "full", "sing-box")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := legacy.ArtifactIdentity(selected[0])
	if err != nil || identity.CoreProvider != (CoreProviderSource{}) {
		t.Fatal("legacy release invented exact core source identity")
	}
	legacyJSON, err := json.Marshal(identity)
	if err != nil || bytes.Contains(legacyJSON, []byte(`"coreProvider"`)) {
		t.Fatal("legacy artifact projection acquired a provider wire field")
	}
}

func TestProviderReleaseMetadataCannotBeAmbiguousOrIncomplete(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"legacy-sources":               func(m *Manifest) { m.Schema = SchemaV1 },
		"missing-sources":              func(m *Manifest) { m.CoreProviders = nil },
		"duplicate-source":             func(m *Manifest) { m.CoreProviders[1] = m.CoreProviders[0] },
		"unknown-source":               func(m *Manifest) { m.CoreProviders[0].ID = "unknown" },
		"missing-artifact-provider":    func(m *Manifest) { m.Artifacts[0].CoreProvider = "" },
		"undeclared-artifact-provider": func(m *Manifest) { m.Artifacts[0].CoreProvider = "unknown" },
		"unused-source":                func(m *Manifest) { m.Artifacts = m.Artifacts[:3] },
		"asymmetric-provider":          func(m *Manifest) { m.Artifacts = m.Artifacts[:4] },
		"cross-provider-profile":       func(m *Manifest) { m.Artifacts[0].CoreProvider = "shtorm-extended" },
		"catalog-claims-provider":      func(m *Manifest) { m.Artifacts[2].CoreProvider = "sing-box" },
		"ambiguous-name":               func(m *Manifest) { m.Artifacts[3].Name = m.Artifacts[0].Name },
		"duplicate-role":               func(m *Manifest) { m.Artifacts[3].CoreProvider = "sing-box" },
		"local-module":                 func(m *Manifest) { m.CoreProviders[0].Module = "../credential-sentinel" },
		"source-url-credentials": func(m *Manifest) {
			m.CoreProviders[0].SourceRepository = "https://credential-sentinel@github.com/MalenkiySolovey/sing-box"
		},
		"short-source":     func(m *Manifest) { m.CoreProviders[0].SourceRevision = "b5f1b37" },
		"short-module-sum": func(m *Manifest) { m.CoreProviders[0].ModuleSum = "h1:YQ==" },
		"semantic-missing": func(m *Manifest) { m.CoreProviders[0].RuntimeSemanticVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := providerManifest(t, time.Unix(1_800_000_000, 0))
			mutate(&manifest)
			if _, err := Canonical(manifest); err == nil || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatal("ambiguous/incomplete provider release accepted or echoed")
			}
		})
	}
	legacy := validManifest(time.Unix(1_800_000_000, 0))
	legacy.Artifacts[0].CoreProvider = "sing-box"
	if _, err := Canonical(legacy); err == nil {
		t.Fatal("v1 silently accepted provider-bearing artifacts")
	}
}

func TestProviderReleaseBindsEverySourceFieldAndCopiesConcurrentProjections(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, mutate := range []func(*CoreProviderSource){
		func(s *CoreProviderSource) { s.ID = "sing-box" },
		func(s *CoreProviderSource) { s.Name = "changed" },
		func(s *CoreProviderSource) { s.SourceRevision = strings.Repeat("0", 40) },
		func(s *CoreProviderSource) { s.ProviderVersion = "v1.14.4-future" },
		func(s *CoreProviderSource) { s.RuntimeSemanticVersion = "9.9.9" },
		func(s *CoreProviderSource) { s.Module = "github.com/shtorm-7/sing-box-extended" },
		func(s *CoreProviderSource) { s.ModuleSum = "h1:" + strings.Repeat("A", 43) + "=" },
		func(s *CoreProviderSource) { s.SourceRepository = "https://github.com/shtorm-7/sing-box-extended" },
		func(s *CoreProviderSource) { s.UpstreamRepository = "https://github.com/SagerNet/sing-box" },
		func(s *CoreProviderSource) { s.UpstreamRevision = strings.Repeat("0", 40) },
	} {
		verified := verifiedIdentityFixture(t, providerManifest(t, now), now)
		mutate(&verified.Manifest.CoreProviders[0])
		if _, err := verified.PanelArtifactsForProvider("linux", "amd64", "full", "shtorm-extended"); err == nil {
			t.Fatal("changed source metadata retained publisher authority")
		}
	}
	verified := verifiedIdentityFixture(t, providerManifest(t, now), now)
	selected, err := verified.PanelArtifactsForProvider("linux", "amd64", "full", "shtorm-extended")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := verified.ArtifactIdentity(selected[0])
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			identity, err := verified.ArtifactIdentity(selected[0])
			if err != nil || !reflect.DeepEqual(identity, expected) {
				t.Error("concurrent provider artifact identity changed")
			}
		})
	}
	workers.Wait()
	// The release reader must be able to represent a signed future immutable
	// source; target capability/runtime qualification stays with its own owner.
	future := providerManifest(t, now)
	future.CoreProviders[0].SourceRevision = strings.Repeat("1", 40)
	future.CoreProviders[0].ProviderVersion = "v1.14.4-0.20261024000000-111111111111"
	if _, err := Canonical(future); err != nil {
		t.Fatalf("reader confused source shape with current runtime qualification: %v", err)
	}
}

func TestProviderSourceRejectsTheOtherProvidersCopy(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		source := providerfixture.Identity(t, id).Source
		if err := ValidateCoreProviderSource(source); err != nil {
			t.Fatal(err)
		}
		wrongCopy := "github.com/MalenkiySolovey/sing-box-extended"
		if id == "shtorm-extended" {
			wrongCopy = "github.com/MalenkiySolovey/sing-box"
		}
		source.Module, source.SourceRepository = wrongCopy, "https://"+wrongCopy
		if err := ValidateCoreProviderSource(source); err == nil {
			t.Fatal("provider accepted the other core copy")
		}
	}
}

func TestProviderEnvelopeRejectsAmbiguousSchemasAndUnknownCriticalFields(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	store, err := NewTrustStore([]TrustRoot{{KeyID: "fixture", PublicKey: private.Public().(ed25519.PublicKey),
		State: RootActive, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(providerManifest(t, now), "fixture", private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(signed, store, now, ChannelMain, 0); err != nil {
		t.Fatal(err)
	}
	var envelope Envelope
	if err := json.Unmarshal(signed, &envelope); err != nil {
		t.Fatal(err)
	}
	unknownSource := envelope
	unknownSource.Manifest = bytes.Replace(envelope.Manifest, []byte(`"coreProviders":[{`), []byte(`"coreProviders":[{"credential-sentinel":true,`), 1)
	if bytes.Equal(unknownSource.Manifest, envelope.Manifest) {
		t.Fatal("source mutation fixture did not change")
	}
	unknownSource.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, unknownSource.Manifest))
	unknownSourceRaw, err := json.Marshal(unknownSource)
	if err != nil {
		t.Fatal(err)
	}
	mismatched := envelope
	mismatched.Schema = SchemaV1
	mismatchedRaw, err := json.Marshal(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"envelope-schema":                   mismatchedRaw,
		"duplicate-envelope-member":         bytes.Replace(signed, []byte(`"schema":`), []byte(`"schema":"solovey.release/v2","schema":`), 1),
		"escaped-duplicate-envelope-member": bytes.Replace(signed, []byte(`"schema":`), []byte(`"\u0073chema":"solovey.release/v2","schema":`), 1),
		"duplicate-signed-source-inventory": bytes.Replace(signed, []byte(`"coreProviders":`), []byte(`"coreProviders":[],"coreProviders":`), 1),
		"unknown-envelope-field":            append([]byte(`{"credential-sentinel":true,`), signed[1:]...),
		"unknown-signed-source-field":       unknownSourceRaw,
		"trailing-document":                 append(slices.Clone(signed), []byte(` {"credential-sentinel":true}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(raw, store, now, ChannelMain, 0); err == nil || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatal("ambiguous/unknown signed metadata was accepted or echoed")
			}
		})
	}
}
