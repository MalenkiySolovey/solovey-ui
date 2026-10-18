package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func verifiedIdentityFixture(t *testing.T, manifest Manifest, now time.Time) Verified {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Sign(manifest, "release-test", private)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTrustStore([]TrustRoot{{KeyID: "release-test", PublicKey: public, State: RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(raw, store, now, ChannelMain, 0)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func TestArtifactIdentityRejectsUnverifiedAndAlteredPublisherMetadata(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verified := verifiedIdentityFixture(t, validManifest(now), now)
	artifact := verified.Manifest.Artifacts[0]
	forged := Verified{Manifest: verified.Manifest, Canonical: append([]byte(nil), verified.Canonical...), Digest: verified.Digest, KeyID: verified.KeyID}
	if _, err := forged.ArtifactIdentity(artifact); err == nil {
		t.Fatal("SHA and public fields manufactured publisher trust")
	}
	if _, err := forged.PanelArtifacts("linux", "amd64", "full"); err == nil {
		t.Fatal("unverified panel selected")
	}
	if _, err := forged.ComponentArtifacts(); err == nil {
		t.Fatal("unverified catalog selected")
	}
	for name, mutate := range map[string]func(*Verified){
		"manifest":  func(v *Verified) { v.Manifest.Version = "2026.3.1" },
		"digest":    func(v *Verified) { v.Digest = digestFor("other") },
		"signer":    func(v *Verified) { v.KeyID = "another-root" },
		"canonical": func(v *Verified) { v.Canonical = []byte("{}") },
	} {
		t.Run(name, func(t *testing.T) {
			copyVerified := verified
			mutate(&copyVerified)
			if _, err := copyVerified.ArtifactIdentity(artifact); err == nil {
				t.Fatal("altered signature binding accepted")
			}
		})
	}
	changed := artifact
	changed.Size++
	if _, err := verified.ArtifactIdentity(changed); err == nil {
		t.Fatal("outside artifact accepted")
	}
	components, err := verified.ComponentArtifacts()
	if err != nil || len(components) != 1 || components[0].Artifact.Kind != "component-catalog" {
		t.Fatalf("catalog binding: %+v %v", components, err)
	}
	wrong := validManifest(now)
	wrong.Components[0].ArtifactSHA256 = wrong.Artifacts[0].SHA256
	if _, err := verifiedIdentityFixture(t, wrong, now).ComponentArtifacts(); err == nil {
		t.Fatal("panel archive promoted to component catalog")
	}
}

func TestArtifactIdentitySurvivesPinnedMirrorTransportAndConcurrentResolution(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	manifest := validManifest(now)
	contents := map[string][]byte{}
	for index := range manifest.Artifacts {
		artifact := &manifest.Artifacts[index]
		contents[artifact.Name] = []byte("deterministic bytes for " + artifact.Role)
		artifact.Size = int64(len(contents[artifact.Name]))
		artifact.SHA256 = digestBytes(contents[artifact.Name])
		if artifact.Role == "component-catalog" {
			manifest.Components[0].ArtifactSHA256 = artifact.SHA256
		}
	}
	verified := verifiedIdentityFixture(t, manifest, now)
	makeServer := func() *httptest.Server {
		return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			content, ok := contents[strings.TrimPrefix(r.URL.Path, "/release/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(content)
		}))
	}
	first, mirror := makeServer(), makeServer()
	defer first.Close()
	defer mirror.Close()
	for _, artifact := range manifest.Artifacts {
		if artifact.Role == "panel-core" {
			continue
		}
		identity, err := verified.ArtifactIdentity(artifact)
		if err != nil {
			t.Fatal(err)
		}
		serialized, _ := json.Marshal(identity)
		if bytes.Contains(serialized, []byte("https:")) || bytes.Contains(serialized, []byte(artifact.Name)) {
			t.Fatal("transport became artifact identity")
		}
		var workers sync.WaitGroup
		for index := range 8 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				server := first
				if index%2 == 1 {
					server = mirror
				}
				resolved, err := verified.ArtifactIdentity(artifact)
				if err != nil || resolved != identity {
					t.Errorf("concurrent identity changed: %v", err)
					return
				}
				source := Source{ID: "pinned-mirror", Origin: server.URL, ManifestPath: "/release/release.json", ExpectedProvenance: "test"}
				var downloaded bytes.Buffer
				if _, err := (Fetcher{Client: server.Client()}).FetchArtifact(t.Context(), source, artifact, &downloaded); err != nil ||
					!bytes.Equal(downloaded.Bytes(), contents[artifact.Name]) {
					t.Errorf("same signed artifact through pinned transport: %v", err)
				}
			}()
		}
		workers.Wait()
	}
}
