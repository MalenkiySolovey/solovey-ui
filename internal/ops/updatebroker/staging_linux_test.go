//go:build linux

package updatebroker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestStagingRejectedChunkDoesNotChangeAcceptedBytesAndExactFinalReplays(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	now := time.Unix(20_000, 0).UTC()
	host := NewHost()
	host.Now = func() time.Time { return now }
	host.InboxRoot = filepath.Join(t.TempDir(), "inbox")
	data := []byte("abcd")
	release := stagingReleaseForBytes(data)
	operationID := "update-operation:staging-boundary"
	result, err := stagingStage(t, host, operationID, release, 0, data[:3], false)
	if err != nil || result.AcceptedBytes != 3 || result.Complete {
		t.Fatalf("first stage=%#v err=%v", result, err)
	}
	artifactPath := stagingArtifactPath(host, operationID, release.Artifacts[0])
	before, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stagingStage(t, host, operationID, release, 3, []byte("de"), false); err == nil {
		t.Fatal("one-byte-over-boundary chunk was accepted")
	}
	after, err := os.ReadFile(artifactPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("rejected write changed bytes: before=%q after=%q err=%v", before, after, err)
	}
	result, err = stagingStage(t, host, operationID, release, 3, data[3:], true)
	if err != nil || result.AcceptedBytes != 4 || !result.Complete || result.ArtifactDigest != release.Artifacts[0].SHA256 {
		t.Fatalf("final stage=%#v err=%v", result, err)
	}
	restarted := NewHost()
	restarted.Now = host.Now
	restarted.InboxRoot = host.InboxRoot
	result, err = stagingStage(t, restarted, operationID, release, 3, data[3:], true)
	if err != nil || !result.Complete || result.AcceptedBytes != 4 {
		t.Fatalf("restart replay=%#v err=%v", result, err)
	}
}

func TestStagingAcknowledgedStageFaultsRetryToDurablyReopenableBytes(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	fault := errors.New("staging inbox durability fault")
	for _, faultAt := range []string{"inbox-root-parent-sync", "operation-parent-sync", "metadata-write", "artifact-create", "artifact-write", "artifact-sync", "artifact-close", "artifact-directory-sync"} {
		t.Run(faultAt, func(t *testing.T) {
			host := NewHost()
			host.Now = func() time.Time { return time.Unix(21_000, 0).UTC() }
			host.InboxRoot = filepath.Join(t.TempDir(), "inbox")
			release := stagingReleaseForBytes([]byte("stage"))
			operationID := "update-operation:staging-fault-" + faultAt
			stagingInjectInboxFault(host, operationID, faultAt, fault)
			if _, err := stagingStage(t, host, operationID, release, 0, []byte("stage"), true); err == nil {
				t.Fatal("faulted stage was acknowledged")
			}
			host.atomicOps = productionUpdateAtomicOps
			host.inboxOps = productionUpdateInboxOps
			result, err := stagingStage(t, host, operationID, release, 0, []byte("stage"), true)
			if err != nil || !result.Complete || result.AcceptedBytes != 5 {
				t.Fatalf("retry=%#v err=%v", result, err)
			}
			restarted := NewHost()
			restarted.Now = host.Now
			restarted.InboxRoot = host.InboxRoot
			result, err = stagingStage(t, restarted, operationID, release, 0, []byte("stage"), true)
			if err != nil || !result.Complete || result.ArtifactDigest != release.Artifacts[0].SHA256 {
				t.Fatalf("restart replay=%#v err=%v", result, err)
			}
		})
	}
}

func TestStagingExactFinalChunkRemainsPreparableAfterRestart(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	root := t.TempDir()
	archive, identity := publicationCoreArchive(t, root)
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	host := NewHost()
	host.Now = func() time.Time { return time.Unix(22_000, 0).UTC() }
	host.InboxRoot = filepath.Join(root, "inbox")
	host.ReleaseRoot = filepath.Join(root, "releases")
	host.StatePath = filepath.Join(root, "state", "update-state.json")
	operationID := "update-operation:staging-restart-prepare"
	cut := len(data) / 2
	if _, err := stagingStage(t, host, operationID, identity, 0, data[:cut], false); err != nil {
		t.Fatal(err)
	}
	restarted := NewHost()
	restarted.Now = host.Now
	restarted.InboxRoot, restarted.ReleaseRoot, restarted.StatePath = host.InboxRoot, host.ReleaseRoot, host.StatePath
	if _, err := stagingStage(t, restarted, operationID, identity, int64(cut), data[cut:], true); err != nil {
		t.Fatal(err)
	}
	payload, digest, err := broker.MarshalPayload(PrepareRequestV1{Release: identity, ExpectedBrokerCapability: broker.CapabilityRevision,
		ExpectedManagementRevision: identity.DeploymentRevision})
	if err != nil {
		t.Fatal(err)
	}
	result, err := restarted.prepare(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest}, broker.PeerIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	prepared := result.(PrepareResultV1)
	if !prepared.ManagementReady || !validDigest(prepared.PreparedRef) || !validDigest(prepared.RollbackRef) {
		t.Fatalf("prepare=%#v", prepared)
	}
	if _, err := os.Lstat(filepath.Join(restarted.ReleaseRoot, releaseDirectoryName(identity))); err != nil {
		t.Fatalf("published release missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(restarted.InboxRoot, operationID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared inbox survived: %v", err)
	}
}

func TestStagingStartupSweepBoundsOwnedInboxesAndPreservesForeignState(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	now := time.Unix(30_000, 0).UTC()
	host := NewHost()
	host.Now = func() time.Time { return now }
	host.InboxRoot = filepath.Join(t.TempDir(), "inbox")
	release := stagingReleaseForBytes([]byte("partial"))
	newest := stagingOwnedInbox(t, host, "update-operation:staging-newest", release, now.Add(-time.Hour), 7)
	older := stagingOwnedInbox(t, host, "update-operation:staging-older", release, now.Add(-2*time.Hour), 7)
	stale := stagingOwnedInbox(t, host, "update-operation:staging-stale", release, now.Add(-25*time.Hour), 7)
	oversized := stagingOwnedInbox(t, host, "update-operation:staging-oversized", release, now.Add(-30*time.Minute), 0)
	if err := os.Truncate(filepath.Join(oversized, "panel-full-partial.tar.gz"), maxResumableInboxBytes+1); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(host.InboxRoot, "foreign-directory")
	ambiguous := filepath.Join(host.InboxRoot, "update-operation:staging-foreign-without-metadata")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ambiguous, 0o700); err != nil {
		t.Fatal(err)
	}
	terminal := stagingOwnedInbox(t, host, "update-operation:staging-terminal", release, now.Add(-10*time.Minute), 7)
	_ = terminal
	releasePayload, releaseDigest, err := broker.MarshalPayload(ReleaseStagingRequestV1{ManifestDigest: release.ManifestDigest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.releaseStaging(context.Background(), broker.Request{OperationID: "update-operation:staging-terminal", Payload: releasePayload, PayloadDigest: releaseDigest}, broker.PeerIdentity{}); err != nil {
		t.Fatal(err)
	}
	if err := host.sweepOperationInboxes(""); err != nil {
		t.Fatal(err)
	}
	for path, exists := range map[string]bool{newest: true, older: false, stale: false, oversized: false, foreign: true, ambiguous: true, terminal: false} {
		_, statErr := os.Lstat(path)
		if got := statErr == nil; got != exists {
			t.Fatalf("path=%s exists=%v want=%v err=%v", path, got, exists, statErr)
		}
	}
	newRelease := stagingReleaseForBytes([]byte("next"))
	if _, err := stagingStage(t, host, "update-operation:staging-next", newRelease, 0, []byte("next"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(newest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded resumable inbox survived next admission: %v", err)
	}
}

func TestStagingStartupSweepRemovesOnlyExactIncompleteGenerations(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	root := t.TempDir()
	host := NewHost()
	host.ReleaseRoot = filepath.Join(root, "releases")
	if err := os.Mkdir(host.ReleaseRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	active := "00000000000000000010-aaaaaaaaaaaaaaaa"
	rollback := "00000000000000000009-bbbbbbbbbbbbbbbb"
	foreign := filepath.Join(host.ReleaseRoot, "operator-recovery")
	for _, path := range []string{filepath.Join(host.ReleaseRoot, active), filepath.Join(host.ReleaseRoot, rollback), foreign} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(active, filepath.Join(host.ReleaseRoot, "current")); err != nil {
		t.Fatal(err)
	}
	staging := []string{
		filepath.Join(host.ReleaseRoot, "00000000000000000011-cccccccccccccccc.staging"),
		filepath.Join(host.ReleaseRoot, "00000000000000000012-dddddddddddddddd.staging"),
		filepath.Join(host.ReleaseRoot, "installer-eeeeeeeeeeeeeeee.staging"),
	}
	for _, path := range staging {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "partial"), []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := host.sweepIncompleteGenerations(); err != nil {
		t.Fatal(err)
	}
	for _, path := range staging {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("incomplete generation survived: %s %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(host.ReleaseRoot, active), filepath.Join(host.ReleaseRoot, rollback), filepath.Join(host.ReleaseRoot, "current"), foreign} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("live/foreign authority was removed: %s %v", path, err)
		}
	}
}

type stagingFaultStageFile struct {
	*os.File
	faultAt string
	fault   error
}

func (f *stagingFaultStageFile) WriteAt(data []byte, offset int64) (int, error) {
	if f.faultAt == "artifact-write" {
		return 0, f.fault
	}
	return f.File.WriteAt(data, offset)
}

func (f *stagingFaultStageFile) Sync() error {
	if f.faultAt == "artifact-sync" {
		return f.fault
	}
	return f.File.Sync()
}

func (f *stagingFaultStageFile) Close() error {
	err := f.File.Close()
	if f.faultAt == "artifact-close" && err == nil {
		return f.fault
	}
	return err
}

func stagingInjectInboxFault(host *Host, operationID, faultAt string, fault error) {
	if faultAt == "metadata-write" {
		host.atomicOps = publicationFaultAtomicOps("write", fault)
		return
	}
	originalEnsure := host.inboxOps.ensureDirectory
	host.inboxOps.ensureDirectory = func(path string, mode os.FileMode) error {
		matches := faultAt == "inbox-root-parent-sync" && path == host.InboxRoot ||
			faultAt == "operation-parent-sync" && path == filepath.Join(host.InboxRoot, operationID)
		if !matches {
			return originalEnsure(path, mode)
		}
		if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		return fault
	}
	originalOpen := host.inboxOps.openFile
	host.inboxOps.openFile = func(path string, flags int, mode os.FileMode) (updateStageFile, error) {
		if faultAt == "artifact-create" {
			return nil, fault
		}
		file, err := originalOpen(path, flags, mode)
		if err != nil {
			return nil, err
		}
		return &stagingFaultStageFile{File: file.(*os.File), faultAt: faultAt, fault: fault}, nil
	}
	if faultAt == "artifact-directory-sync" {
		host.inboxOps.syncDirectory = func(path string) error {
			if path == filepath.Join(host.InboxRoot, operationID) {
				return fault
			}
			return syncDirectory(path)
		}
	}
}

func stagingReleaseForBytes(data []byte) ReleaseIdentityV1 {
	artifact := ArtifactIdentityV1{Name: "payload.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64", MediaType: "application/gzip",
		Size: int64(len(data)), SHA256: updateDigest(string(data)), Provenance: "release-ci"}
	return ReleaseIdentityV1{ReleaseID: "solovey-ui-main-staging", Sequence: 11, Version: "2026.5.0", ManifestDigest: updateDigest("staging-manifest-" + string(data)),
		ArtifactSetDigest: artifactSetDigest([]ArtifactIdentityV1{artifact}), BinaryProfile: "full", DeploymentRevision: updateDigest("staging-deployment"),
		MigrationSetDigest: updateDigest("staging-migration"), RestartClass: "stack", RollbackClass: "automatic", Artifacts: []ArtifactIdentityV1{artifact}}
}

func stagingStage(t *testing.T, host *Host, operationID string, release ReleaseIdentityV1, offset int64, chunk []byte, final bool) (StageChunkResultV1, error) {
	t.Helper()
	payload, digest, err := broker.MarshalPayload(StageChunkRequestV1{Release: release, Artifact: release.Artifacts[0], Offset: offset, Chunk: chunk, Final: final})
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.stage(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest,
		Expected: broker.Revisions{Configuration: release.DeploymentRevision}}, broker.PeerIdentity{})
	if err != nil {
		return StageChunkResultV1{}, err
	}
	return result.(StageChunkResultV1), nil
}

func stagingArtifactPath(host *Host, operationID string, artifact ArtifactIdentityV1) string {
	return filepath.Join(host.InboxRoot, operationID, artifact.Role+"-"+artifact.Name)
}

func stagingOwnedInbox(t *testing.T, host *Host, operationID string, release ReleaseIdentityV1, modified time.Time, bytes int64) string {
	t.Helper()
	root, err := host.operationInbox(operationID, release, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "panel-full-partial.tar.gz")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(bytes); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(root, func(path string, _ os.FileInfo, walkErr error) error {
		if walkErr == nil {
			_ = os.Chtimes(path, modified, modified)
		}
		return walkErr
	})
	return root
}
