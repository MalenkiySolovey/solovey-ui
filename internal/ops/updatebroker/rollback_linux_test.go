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

type rollbackBrokerFixture struct {
	host        *Host
	root        string
	predecessor ReleaseIdentityV1
	applied     ReleaseIdentityV1
	next        ReleaseIdentityV1
	afterNext   ReleaseIdentityV1
	archives    map[uint64]string
}

func TestRollbackPreparedRollbackPreservesLiveAuthorityAcrossFailureAndRollback(t *testing.T) {
	t.Run("activation failure consumes only prepared authority", func(t *testing.T) {
		fixture := newRollbackBrokerFixture(t)
		operationID := "update-operation:rollback-failed-b"
		initial := fixture.load(t)
		if initial.hasPreparedRollbackBinding() || !initial.hasBoundRollback() || initial.RollbackSequence != fixture.predecessor.Sequence {
			t.Fatalf("B interrupted before Prepare changed A-to-P authority: %#v", initial)
		}
		fixture.stageAndPrepare(t, operationID, fixture.next)
		assertRollbackPreparedAndLiveAuthority(t, fixture.host, operationID, fixture.next, fixture.applied, fixture.predecessor)

		fixture.host.OwnerManifest = func(_ context.Context, releaseRoot, _ string) error {
			if filepath.Base(releaseRoot) == releaseDirectoryName(fixture.next) {
				return errors.New("forced owner publication failure")
			}
			return nil
		}
		if _, err := fixture.activate(operationID, fixture.next); err == nil {
			t.Fatal("activation fault was accepted")
		}
		assertRollbackPreparedAndLiveAuthority(t, fixture.host, operationID, fixture.next, fixture.applied, fixture.predecessor)

		if _, err := fixture.rollback(operationID, fixture.next); err != nil {
			t.Fatal(err)
		}
		state := fixture.load(t)
		if state.ActiveSequence != fixture.applied.Sequence || state.ActiveDigest != fixture.applied.ManifestDigest ||
			state.hasPreparedRollbackBinding() || !state.rollbackMatches("update-operation:rollback-applied-a", fixture.applied.ManifestDigest,
			semanticRef("rollback", "update-operation:rollback-applied-a", fixture.applied.ManifestDigest)) ||
			state.RollbackSequence != fixture.predecessor.Sequence || state.RollbackDigest != fixture.predecessor.ManifestDigest {
			t.Fatalf("failed B consumed A-to-P authority: %#v", state)
		}
	})

	t.Run("activated rollback keeps durable high-water", func(t *testing.T) {
		fixture := newRollbackBrokerFixture(t)
		operationID := "update-operation:rollback-activated-b"
		fixture.stageAndPrepare(t, operationID, fixture.next)
		if _, err := fixture.activate(operationID, fixture.next); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.rollback(operationID, fixture.next); err != nil {
			t.Fatal(err)
		}
		state := fixture.load(t)
		if state.ActiveSequence != fixture.applied.Sequence || state.MaxActivatedSequence != fixture.next.Sequence ||
			state.MaxActivatedDigest != fixture.next.ManifestDigest || !state.hasBoundRollback() || state.hasPreparedRollbackBinding() {
			t.Fatalf("rollback lowered the high-water or revoked A-to-P: %#v", state)
		}

		restarted := NewHost()
		restarted.InboxRoot, restarted.ReleaseRoot, restarted.StatePath, restarted.Manifest = fixture.host.InboxRoot, fixture.host.ReleaseRoot, fixture.host.StatePath, fixture.host.Manifest
		restarted.OwnerManifest = func(context.Context, string, string) error { return nil }
		fixture.host = restarted
		if reopened := fixture.load(t); reopened.MaxActivatedSequence != fixture.next.Sequence || reopened.ActiveSequence != fixture.applied.Sequence {
			t.Fatalf("restart lost rollback/high-water separation: %#v", reopened)
		}

		fixture.stage(t, "update-operation:rollback-repeat-b", fixture.next)
		if _, err := fixture.prepare("update-operation:rollback-repeat-b", fixture.next); !brokerErrorCode(err, broker.CodeRevision) {
			t.Fatalf("sequence N was accepted after N-to-N-1 rollback: %v", err)
		}
		fixture.stageAndPrepare(t, "update-operation:rollback-next-c", fixture.afterNext)
		advanced := fixture.load(t)
		if advanced.MaxActivatedSequence != fixture.next.Sequence ||
			!advanced.preparedRollbackMatches("update-operation:rollback-next-c", fixture.afterNext.ManifestDigest,
				semanticRef("rollback", "update-operation:rollback-next-c", fixture.afterNext.ManifestDigest)) {
			t.Fatalf("N+1 did not advance from the preserved fence: %#v", advanced)
		}
	})
}

func TestRollbackSuccessfulVerificationPromotesOnlyItsPreparedAuthority(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	self, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	releaseRoot, releaseName := filepath.Dir(filepath.Dir(self)), filepath.Base(filepath.Dir(self))
	identity := validHostReleaseIdentity()
	identity.Sequence, identity.ReleaseID, identity.ManifestDigest = 12, "solovey-ui-main-12", updateDigest("rollback-running-manifest")
	identityPath := filepath.Join(filepath.Dir(self), "release-identity.json")
	if _, err := os.Lstat(identityPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("test executable directory already contains release identity: %v", err)
	}
	encoded, err := json.Marshal(identity)
	if err != nil || os.WriteFile(identityPath, encoded, 0o600) != nil {
		t.Fatalf("write running release identity: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(identityPath) })

	operationID := "update-operation:rollback-verified-b"
	rollbackRef := semanticRef("rollback", operationID, identity.ManifestDigest)
	host := NewHost()
	host.ReleaseRoot, host.StatePath = releaseRoot, filepath.Join(t.TempDir(), "update-state.json")
	state := diskState{ActiveRelease: releaseName, ActiveSequence: identity.Sequence, ActiveDigest: identity.ManifestDigest,
		MaxActivatedSequence: identity.Sequence, MaxActivatedDigest: identity.ManifestDigest,
		PreparedRollbackRelease: "00000000000000000011-1111111111111111", PreparedRollbackSequence: 11, PreparedRollbackDigest: updateDigest("rollback-a"),
		PreparedRollbackOperationID: operationID, PreparedRollbackManifestDigest: identity.ManifestDigest, PreparedRollbackRef: rollbackRef}
	if err := host.saveState(state); err != nil {
		t.Fatal(err)
	}
	payload, digest, err := broker.MarshalPayload(VerifyRequestV1{Release: identity,
		PreparedRef: semanticRef("prepared", operationID, identity.ManifestDigest), RollbackRef: rollbackRef})
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.verify(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest},
		broker.PeerIdentity{PID: os.Getpid(), Executable: self})
	if err != nil {
		t.Fatal(err)
	}
	verified := result.(VerifyResultV1)
	reopened, err := host.loadState()
	if err != nil || !verified.Verified || reopened.VerifiedSequence != identity.Sequence || reopened.hasPreparedRollbackBinding() ||
		!reopened.rollbackMatches(operationID, identity.ManifestDigest, rollbackRef) || reopened.RollbackSequence != 11 {
		t.Fatalf("verified result=%#v state=%#v err=%v", verified, reopened, err)
	}
}

func newRollbackBrokerFixture(t *testing.T) *rollbackBrokerFixture {
	t.Helper()
	requireRootUpdateBrokerTest(t)
	root, err := os.MkdirTemp("/", "solovey-update-rollback-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	host := NewHost()
	host.InboxRoot, host.ReleaseRoot = filepath.Join(root, "inbox"), filepath.Join(root, "releases")
	host.StatePath, host.Manifest = filepath.Join(root, "state", "update-state.json"), filepath.Join(root, "authority", "broker-clients.json")
	host.OwnerManifest = func(context.Context, string, string) error { return nil }
	host.Restart = nil
	for _, directory := range []string{filepath.Dir(host.StatePath), filepath.Dir(host.Manifest), filepath.Join(root, "archives")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fixture := &rollbackBrokerFixture{host: host, root: root, archives: map[uint64]string{}}
	fixture.archives[8], fixture.predecessor = publicationArchive(t, filepath.Join(root, "archives"), "full", 8)
	fixture.archives[9], fixture.applied = publicationArchive(t, filepath.Join(root, "archives"), "full", 9)
	fixture.archives[10], fixture.next = publicationArchive(t, filepath.Join(root, "archives"), "full", 10)
	fixture.archives[11], fixture.afterNext = publicationArchive(t, filepath.Join(root, "archives"), "full", 11)
	for _, identity := range []ReleaseIdentityV1{fixture.predecessor, fixture.applied} {
		if err := host.publishPreparedGeneration(fixture.archives[identity.Sequence], filepath.Join(host.ReleaseRoot, releaseDirectoryName(identity)), identity); err != nil {
			t.Fatal(err)
		}
	}
	if err := host.activateLink(releaseDirectoryName(fixture.applied)); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(publicationManifest(t, filepath.Join(host.ReleaseRoot, releaseDirectoryName(fixture.applied), "solovey-ui")))
	if err != nil {
		t.Fatal(err)
	}
	if err := publishUpdateAuthority(productionUpdateAtomicOps, host.Manifest, append(manifest, '\n'), 0o640, 0, 0, func() error {
		_, err := broker.LoadManifest(host.Manifest)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	appliedOperation := "update-operation:rollback-applied-a"
	liveRef := semanticRef("rollback", appliedOperation, fixture.applied.ManifestDigest)
	state := diskState{ActiveRelease: releaseDirectoryName(fixture.applied), ActiveSequence: fixture.applied.Sequence, ActiveDigest: fixture.applied.ManifestDigest,
		VerifiedSequence: fixture.applied.Sequence, VerifiedDigest: fixture.applied.ManifestDigest,
		MaxActivatedSequence: fixture.applied.Sequence, MaxActivatedDigest: fixture.applied.ManifestDigest,
		RollbackRelease: releaseDirectoryName(fixture.predecessor), RollbackSequence: fixture.predecessor.Sequence, RollbackDigest: fixture.predecessor.ManifestDigest,
		RollbackOperationID: appliedOperation, RollbackManifestDigest: fixture.applied.ManifestDigest, RollbackRef: liveRef}
	if err := host.saveState(state); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *rollbackBrokerFixture) stageAndPrepare(t *testing.T, operationID string, identity ReleaseIdentityV1) {
	t.Helper()
	fixture.stage(t, operationID, identity)
	if _, err := fixture.prepare(operationID, identity); err != nil {
		t.Fatal(err)
	}
}

func (fixture *rollbackBrokerFixture) stage(t *testing.T, operationID string, identity ReleaseIdentityV1) {
	t.Helper()
	data, err := os.ReadFile(fixture.archives[identity.Sequence])
	if err != nil {
		t.Fatal(err)
	}
	payload, digest, err := broker.MarshalPayload(StageChunkRequestV1{Release: identity, Artifact: identity.Artifacts[0], Chunk: data, Final: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.host.stage(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest,
		Expected: broker.Revisions{Configuration: identity.DeploymentRevision}}, broker.PeerIdentity{}); err != nil {
		t.Fatal(err)
	}
}

func (fixture *rollbackBrokerFixture) prepare(operationID string, identity ReleaseIdentityV1) (any, error) {
	payload, digest, err := broker.MarshalPayload(PrepareRequestV1{Release: identity, ExpectedBrokerCapability: broker.CapabilityRevision,
		ExpectedManagementRevision: identity.DeploymentRevision})
	if err != nil {
		return nil, err
	}
	return fixture.host.prepare(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest}, broker.PeerIdentity{})
}

func (fixture *rollbackBrokerFixture) activate(operationID string, identity ReleaseIdentityV1) (any, error) {
	payload, digest, err := broker.MarshalPayload(ActivateRequestV1{Release: identity,
		PreparedRef: semanticRef("prepared", operationID, identity.ManifestDigest),
		RollbackRef: semanticRef("rollback", operationID, identity.ManifestDigest), ExpectedMode: "native"})
	if err != nil {
		return nil, err
	}
	return fixture.host.activate(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest}, broker.PeerIdentity{PID: 42})
}

func (fixture *rollbackBrokerFixture) rollback(operationID string, identity ReleaseIdentityV1) (any, error) {
	payload, digest, err := broker.MarshalPayload(RollbackRequestV1{Release: identity,
		RollbackRef: semanticRef("rollback", operationID, identity.ManifestDigest), ReasonCode: "rollback-test"})
	if err != nil {
		return nil, err
	}
	return fixture.host.rollback(context.Background(), broker.Request{OperationID: operationID, Payload: payload, PayloadDigest: digest}, broker.PeerIdentity{PID: 42})
}

func (fixture *rollbackBrokerFixture) load(t *testing.T) diskState {
	t.Helper()
	state, err := fixture.host.loadState()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func assertRollbackPreparedAndLiveAuthority(t *testing.T, host *Host, operationID string, source, target, predecessor ReleaseIdentityV1) {
	t.Helper()
	state, err := host.loadState()
	if err != nil || !state.preparedRollbackMatches(operationID, source.ManifestDigest, semanticRef("rollback", operationID, source.ManifestDigest)) ||
		state.PreparedRollbackSequence != target.Sequence || state.PreparedRollbackDigest != target.ManifestDigest ||
		!state.rollbackMatches("update-operation:rollback-applied-a", target.ManifestDigest,
			semanticRef("rollback", "update-operation:rollback-applied-a", target.ManifestDigest)) ||
		state.RollbackSequence != predecessor.Sequence || state.RollbackDigest != predecessor.ManifestDigest {
		t.Fatalf("prepared/live rollback authorities state=%#v err=%v", state, err)
	}
}

func brokerErrorCode(err error, code broker.ErrorCode) bool {
	var public *broker.PublicError
	return errors.As(err, &public) && public.Code == code
}
