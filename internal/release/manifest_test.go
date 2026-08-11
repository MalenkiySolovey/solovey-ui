package release

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignedManifestDeterminismTrustRotationAndReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nextPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := validManifest(now)
	firstCanonical, err := Canonical(manifest)
	if err != nil {
		t.Fatal(err)
	}
	reordered := manifest
	reordered.Artifacts = []Artifact{manifest.Artifacts[2], manifest.Artifacts[0], manifest.Artifacts[1]}
	secondCanonical, err := Canonical(reordered)
	if err != nil || string(firstCanonical) != string(secondCanonical) {
		t.Fatalf("canonical release changed with input order: %v", err)
	}
	envelope, err := Sign(reordered, "release-2026", private)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTrustStore([]TrustRoot{
		{KeyID: "release-2026", PublicKey: public, State: RootActive, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1},
		{KeyID: "release-2027", PublicKey: nextPublic, State: RootNext, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 43},
	})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(envelope, store, now, ChannelMain, 41)
	if err != nil || verified.Manifest.Sequence != 42 || verified.KeyID != "release-2026" || len(verified.Canonical) == 0 {
		t.Fatalf("valid signed release rejected: %#v %v", verified, err)
	}
	if _, err := Verify(envelope, store, now, ChannelMain, 42); err == nil {
		t.Fatal("replayed sequence was accepted")
	}
	if _, err := Verify(envelope, store, now, ChannelBeta, 0); err == nil {
		t.Fatal("cross-channel manifest was accepted")
	}
	if _, err := Verify(envelope, store, now.Add(3*time.Hour), ChannelMain, 0); err == nil {
		t.Fatal("expired manifest was accepted")
	}
	retired, err := NewTrustStore([]TrustRoot{{KeyID: "release-2026", PublicKey: public, State: RootRetired,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(envelope, retired, now, ChannelMain, 0); err == nil {
		t.Fatal("retired signing root was accepted")
	}
	unknown, _ := NewTrustStore([]TrustRoot{{KeyID: "release-2027", PublicKey: nextPublic, State: RootNext,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if _, err := Verify(envelope, unknown, now, ChannelMain, 0); err == nil {
		t.Fatal("unknown signing root was accepted")
	}
	tampered := append([]byte(nil), envelope...)
	for index := range tampered {
		if tampered[index] == '4' && index+1 < len(tampered) && tampered[index+1] == '2' {
			tampered[index+1] = '3'
			break
		}
	}
	if _, err := Verify(tampered, store, now, ChannelMain, 0); err == nil {
		t.Fatal("tampered signed manifest was accepted")
	}
}

func TestManifestRequiresCoherentProfilesAndComponentArtifact(t *testing.T) {
	manifest := validManifest(time.Unix(1_800_000_000, 0))
	full, err := manifest.ArtifactsFor("linux", "amd64", "full")
	if err != nil || len(full) != 1 || full[0].Role != "panel-full" {
		t.Fatalf("full profile selection=%#v err=%v", full, err)
	}
	core, err := manifest.ArtifactsFor("linux", "amd64", "core")
	if err != nil || len(core) != 1 || core[0].Role != "panel-core" {
		t.Fatalf("core profile selection=%#v err=%v", core, err)
	}
	missing := manifest
	missing.Artifacts = []Artifact{manifest.Artifacts[0], manifest.Artifacts[2]}
	if err := missing.Validate(); err == nil {
		t.Fatal("Linux target without both profiles was accepted")
	}
	foreignComponent := manifest
	foreignComponent.Components = append([]Component(nil), manifest.Components...)
	foreignComponent.Components[0].ArtifactSHA256 = digestFor("outside")
	if err := foreignComponent.Validate(); err == nil {
		t.Fatal("component outside the coherent artifact set was accepted")
	}
	duplicateRole := manifest
	duplicate := manifest.Artifacts[0]
	duplicate.Name = "solovey-ui-linux-amd64-copy.tar.gz"
	duplicate.SHA256 = digestFor("full-copy")
	duplicateRole.Artifacts = append(duplicateRole.Artifacts, duplicate)
	if err := duplicateRole.Validate(); err == nil {
		t.Fatal("duplicate artifact role for one target was accepted")
	}
	missingProvenance := manifest
	missingProvenance.Artifacts = append([]Artifact(nil), manifest.Artifacts...)
	missingProvenance.Artifacts[0].Provenance = ""
	if err := missingProvenance.Validate(); err == nil {
		t.Fatal("artifact without the required signed provenance identity was accepted")
	}
	oversizedSet := manifest
	oversizedSet.Artifacts = append([]Artifact(nil), manifest.Artifacts...)
	for index := range oversizedSet.Artifacts {
		oversizedSet.Artifacts[index].Size = 800 << 20
	}
	if err := oversizedSet.Validate(); err == nil {
		t.Fatal("release set beyond the aggregate size budget was accepted")
	}
	if _, err := manifest.ArtifactsFor("linux", "arm64", "full"); err == nil {
		t.Fatal("unsupported architecture was accepted")
	}
	if _, err := manifest.ArtifactsFor("windows", "amd64", "full"); err == nil {
		t.Fatal("unsupported platform was accepted")
	}
	invertedPanel := manifest
	invertedPanel.MinimumPanelVersion, invertedPanel.MaximumPanelVersion = "2026.4.0", "2026.3.0"
	if err := invertedPanel.Validate(); err == nil {
		t.Fatal("inverted panel compatibility range was accepted")
	}
	outsideTarget := manifest
	outsideTarget.MinimumCoreSchema, outsideTarget.TargetCoreSchema = "1.12", "1.11"
	if err := outsideTarget.Validate(); err == nil {
		t.Fatal("target schema outside the declared compatibility range was accepted")
	}
	invertedComponent := manifest
	invertedComponent.Components = append([]Component(nil), manifest.Components...)
	invertedComponent.Components[0].MinimumCoreSchema, invertedComponent.Components[0].MaximumCoreSchema = "1.12", "1.11"
	if err := invertedComponent.Validate(); err == nil {
		t.Fatal("inverted component schema range was accepted")
	}
	headerInjection := manifest
	headerInjection.Artifacts = append([]Artifact(nil), manifest.Artifacts...)
	headerInjection.Artifacts[0].MediaType = "application/gzip\r\nX-Injected: true"
	if err := headerInjection.Validate(); err == nil {
		t.Fatal("invalid artifact media type was accepted")
	}
}

func TestSignedManifestRejectsWrongKeyFutureIssueAndMalformedEnvelope(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	active, err := NewTrustStore([]TrustRoot{{KeyID: "release-2026", PublicKey: public, State: RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1, MaxSequence: 100}})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Sign(validManifest(now), "release-2026", private)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := NewTrustStore([]TrustRoot{{KeyID: "release-2026", PublicKey: wrongPublic, State: RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(envelope, wrong, now, ChannelMain, 0); err == nil {
		t.Fatal("manifest signed by the wrong key was accepted")
	}
	futureManifest := validManifest(now)
	futureManifest.IssuedAt = now.Add(time.Minute).Unix()
	futureManifest.ExpiresAt = now.Add(time.Hour).Unix()
	futureEnvelope, err := Sign(futureManifest, "release-2026", private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(futureEnvelope, active, now, ChannelMain, 0); err == nil {
		t.Fatal("future-issued release metadata was accepted")
	}
	if _, err := Verify([]byte(`{"schema":"solovey.release/v1","unknown":true}`), active, now, ChannelMain, 0); err == nil {
		t.Fatal("malformed envelope with an unknown field was accepted")
	}
	if _, err := Verify(make([]byte, MaxManifestBytes+1), active, now, ChannelMain, 0); err == nil {
		t.Fatal("oversized release metadata was accepted")
	}
	next, err := NewTrustStore([]TrustRoot{{KeyID: "release-2026", PublicKey: public, State: RootNext,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 40, MaxSequence: 42}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(envelope, next, now, ChannelMain, 0); err != nil {
		t.Fatalf("bounded NEXT rotation root rejected its authorized sequence: %v", err)
	}
}

func TestPinnedSourceBoundsOriginRedirectSizeAndDigest(t *testing.T) {
	artifactBytes := []byte("bounded release artifact")
	artifact := Artifact{Name: "solovey-ui-linux-amd64.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64",
		MediaType: "application/octet-stream", Size: int64(len(artifactBytes)), SHA256: digestBytes(artifactBytes), Provenance: "test"}
	assetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(artifactBytes)
	}))
	defer assetServer.Close()
	manifestServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release/manifest.json" {
			_, _ = w.Write([]byte(`{"bounded":true}`))
			return
		}
		http.Redirect(w, r, assetServer.URL+r.URL.Path, http.StatusFound)
	}))
	defer manifestServer.Close()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // #nosec G402 -- isolated TLS fixtures.
	fetcher := Fetcher{Client: client}
	source := Source{ID: "test-source", Origin: manifestServer.URL, ManifestPath: "/release/manifest.json", ExpectedProvenance: "test"}
	raw, err := fetcher.Fetch(context.Background(), source)
	if err != nil || string(raw) != `{"bounded":true}` {
		t.Fatalf("manifest fetch=%q err=%v", raw, err)
	}
	if _, err := fetcher.FetchArtifact(context.Background(), source, artifact, io.Discard); err == nil {
		t.Fatal("redirect to an unpinned artifact origin was accepted")
	}
	source.RedirectOrigins = []string{assetServer.URL}
	written, err := fetcher.FetchArtifact(context.Background(), source, artifact, io.Discard)
	if err != nil || written != artifact.Size {
		t.Fatalf("pinned artifact fetch bytes=%d err=%v", written, err)
	}
	bad := artifact
	bad.SHA256 = digestFor("wrong")
	if _, err := fetcher.FetchArtifact(context.Background(), source, bad, io.Discard); err == nil {
		t.Fatal("artifact with wrong digest was accepted")
	}
	bad = artifact
	bad.Provenance = "foreign-builder"
	if _, err := fetcher.FetchArtifact(context.Background(), source, bad, io.Discard); err == nil {
		t.Fatal("artifact outside the pinned provenance policy was accepted")
	}
}

func TestPinnedSourceEnforcesReadIdleBound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.solovey.release+json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"too":"late"}`))
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // #nosec G402 -- isolated TLS fixture.
	fetcher := Fetcher{Client: client, ReadIdle: 15 * time.Millisecond}
	source := Source{ID: "idle-test", Origin: server.URL, ManifestPath: "/manifest.json", ExpectedProvenance: "test"}
	started := time.Now()
	if _, err := fetcher.Fetch(context.Background(), source); err == nil {
		t.Fatal("release response exceeding the read idle bound was accepted")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("read idle bound took too long: %v", elapsed)
	}
}

func validManifest(now time.Time) Manifest {
	catalogDigest := digestFor("components")
	return Manifest{Schema: SchemaV1, ReleaseID: "solovey-ui-main-42", Sequence: 42, Version: "2026.3.0", Channel: ChannelMain,
		IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), DeploymentRevision: digestFor("deployment"),
		MinimumPanelVersion: "2026.2.0", MaximumPanelVersion: "2026.3.0",
		MinimumCoreSchema: "1.11", MaximumCoreSchema: "1.11", TargetCoreSchema: "1.11", BrokerCapability: "broker-capabilities-1.2",
		MigrationSetDigest: digestFor("migrations"), ReleaseNotesDigest: digestFor("notes"),
		RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic",
		Artifacts: []Artifact{
			{Name: "solovey-ui-linux-amd64.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64", MediaType: "application/gzip", Size: 100, SHA256: digestFor("full"), Provenance: "test"},
			{Name: "solovey-ui-core-linux-amd64.tar.gz", Role: "panel-core", Platform: "linux", Arch: "amd64", MediaType: "application/gzip", Size: 90, SHA256: digestFor("core"), Provenance: "test"},
			{Name: "solovey-ui-components.tar.gz", Role: "component-catalog", Platform: "any", Arch: "any", MediaType: "application/gzip", Size: 80, SHA256: catalogDigest, Provenance: "test"},
		}, Components: []Component{{ID: "panel-update-ui", Version: "1.0.0", ArtifactSHA256: catalogDigest, MinimumCoreSchema: "1.11", MaximumCoreSchema: "1.11"}}}
}

func digestFor(value string) string { return digestBytes([]byte(value)) }

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func BenchmarkSignedManifestVerification(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	envelope, err := Sign(validManifest(now), "release-benchmark", private)
	if err != nil {
		b.Fatal(err)
	}
	store, err := NewTrustStore([]TrustRoot{{KeyID: "release-benchmark", PublicKey: public, State: RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(envelope)))
	for b.Loop() {
		if _, err := Verify(envelope, store, now, ChannelMain, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReleaseSetSelection(b *testing.B) {
	manifest := validManifest(time.Unix(1_800_000_000, 0))
	b.ReportAllocs()
	for b.Loop() {
		artifacts, err := manifest.ArtifactsFor("linux", "amd64", "full")
		if err != nil || len(artifacts) != 1 {
			b.Fatal(err)
		}
	}
}
