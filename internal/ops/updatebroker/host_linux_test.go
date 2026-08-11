//go:build linux

package updatebroker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestBrokerDiskStateAndStoredReleaseIdentityFailClosed(t *testing.T) {
	host := NewHost()
	host.StatePath = filepath.Join(t.TempDir(), "update-state.json")
	unsafe := diskState{ActiveRelease: "../../etc", ActiveSequence: 3, ActiveDigest: updateDigest("manifest")}
	raw, _ := json.Marshal(unsafe)
	if err := os.WriteFile(host.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.loadState(); err == nil {
		t.Fatal("path-traversing broker state was accepted")
	}
	if err := os.WriteFile(host.StatePath, []byte(`{"activeSequence":0,"activeDigest":"","activeRelease":"","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.loadState(); err == nil {
		t.Fatal("broker state with unknown fields was accepted")
	}

	identity := validHostReleaseIdentity()
	root := filepath.Join(t.TempDir(), releaseDirectoryName(identity))
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(identity)
	if err := os.WriteFile(filepath.Join(root, "release-identity.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	stored, err := loadReleaseIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	request := identity
	request.Artifacts = nil
	if !sameReleaseIdentity(stored, request, false) {
		t.Fatal("matching stored release identity was rejected")
	}
	request.MigrationSetDigest = updateDigest("other-migration")
	if sameReleaseIdentity(stored, request, false) {
		t.Fatal("stored release identity drift was accepted")
	}
}

func TestStageRejectsArtifactOutsideDeclaredSet(t *testing.T) {
	host := NewHost()
	host.InboxRoot = filepath.Join(t.TempDir(), "update-inbox")
	identity := validHostReleaseIdentity()
	foreign := identity.Artifacts[0]
	foreign.Name = "other.tar.gz"
	foreign.SHA256 = updateDigest("other")
	payload, digest, err := broker.MarshalPayload(StageChunkRequestV1{Release: identity, Artifact: foreign, Offset: 0, Chunk: []byte("x"), Final: true})
	if err != nil {
		t.Fatal(err)
	}
	request := broker.Request{OperationID: "update-operation:test-stage", Payload: payload, PayloadDigest: digest,
		Expected: broker.Revisions{Configuration: identity.DeploymentRevision}}
	_, err = host.stage(context.Background(), request, broker.PeerIdentity{})
	var public *broker.PublicError
	if !errors.As(err, &public) || public.Code != broker.CodeInvalidRequest {
		t.Fatalf("undeclared artifact error=%v", err)
	}
}

func validHostReleaseIdentity() ReleaseIdentityV1 {
	artifact := ArtifactIdentityV1{Name: "solovey-ui-linux-amd64.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64",
		MediaType: "application/gzip", Size: 1, SHA256: updateDigest("artifact"), Provenance: "release-ci"}
	return ReleaseIdentityV1{ReleaseID: "solovey-ui-main-9", Sequence: 9, Version: "2026.3.0", ManifestDigest: updateDigest("manifest"),
		ArtifactSetDigest: artifactSetDigest([]ArtifactIdentityV1{artifact}), BinaryProfile: "full",
		DeploymentRevision: updateDigest("deployment"), MigrationSetDigest: updateDigest("migration"),
		RestartClass: "stack", RollbackClass: "automatic", Artifacts: []ArtifactIdentityV1{artifact}}
}
