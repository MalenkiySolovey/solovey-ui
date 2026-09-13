//go:build linux

package updatebroker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestPublicationStatePublicationFaultMatrixReopensOldOrNewAuthority(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	directory := t.TempDir()
	host := NewHost()
	host.StatePath = filepath.Join(directory, "update-state.json")
	oldState := diskState{ActiveRelease: "00000000000000000001-1111111111111111", ActiveSequence: 1, ActiveDigest: updateDigest("old")}
	newState := diskState{ActiveRelease: "00000000000000000002-2222222222222222", ActiveSequence: 2, ActiveDigest: updateDigest("new")}
	if err := host.saveState(oldState); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("publication state publication fault")
	for _, faultAt := range []string{"create", "write", "sync", "close", "rename", "sync-dir"} {
		t.Run(faultAt, func(t *testing.T) {
			if err := host.saveState(oldState); err != nil {
				t.Fatal(err)
			}
			host.atomicOps = publicationFaultAtomicOps(faultAt, fault)
			err := host.saveState(newState)
			if !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			if got := updateAuthorityVisible(err); got != (faultAt == "sync-dir") {
				t.Fatalf("visible=%v at %s", got, faultAt)
			}
			restarted := NewHost()
			restarted.StatePath = host.StatePath
			reopened, loadErr := restarted.loadState()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			want := oldState
			if faultAt == "sync-dir" {
				want = newState
			}
			if reopened != want {
				t.Fatalf("reopened=%#v want=%#v", reopened, want)
			}
			host.atomicOps = productionUpdateAtomicOps
		})
	}
}

func TestPublicationRotatedClientManifestFaultMatrixReopensOldOrNewAuthority(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	root, err := os.MkdirTemp("/", "solovey-update-publication-manifest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	oldExecutable := publicationExecutable(t, filepath.Join(root, "old", "solovey-ui"), "old")
	newExecutable := publicationExecutable(t, filepath.Join(root, "new", "solovey-ui"), "new")
	manifestDirectory := filepath.Join(root, "authority")
	if err := os.Mkdir(manifestDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(manifestDirectory, "broker-clients.json")
	seed := func() {
		manifest := publicationManifest(t, oldExecutable)
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := publishUpdateAuthority(productionUpdateAtomicOps, manifestPath, append(data, '\n'), 0o640, 0, 0, func() error {
			_, err := broker.LoadManifest(manifestPath)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed()
	fault := errors.New("publication manifest publication fault")
	for _, faultAt := range []string{"create", "write", "sync", "close", "rename", "sync-dir"} {
		t.Run(faultAt, func(t *testing.T) {
			seed()
			host := NewHost()
			host.Manifest = manifestPath
			host.atomicOps = publicationFaultAtomicOps(faultAt, fault)
			err := host.rewriteClientManifest(filepath.Dir(newExecutable))
			if !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			reopened, loadErr := broker.LoadManifest(manifestPath)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			want := oldExecutable
			if faultAt == "sync-dir" {
				want = newExecutable
			}
			if reopened.Clients[0].Executable != want {
				t.Fatalf("executable=%q want=%q", reopened.Clients[0].Executable, want)
			}
		})
	}
}

func TestPublicationExtractedMemberFaultMatrixRequiresWriteSyncAndClose(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	data := []byte("release-member")
	fault := errors.New("publication member fault")
	for _, faultAt := range []string{"create", "write", "sync", "close"} {
		t.Run(faultAt, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "solovey-ui")
			ops := releaseMemberOps{openFile: func(path string, flags int, mode os.FileMode) (releaseMemberFile, error) {
				if faultAt == "create" {
					return nil, fault
				}
				file, err := os.OpenFile(path, flags, mode)
				if err != nil {
					return nil, err
				}
				return &publicationFaultAtomicFile{File: file, faultAt: faultAt, fault: fault}, nil
			}}
			if _, err := writeExtractedReleaseMember(ops, target, "solovey-ui", bytes.NewReader(data), int64(len(data)), 0o755); !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
		})
	}
	target := filepath.Join(t.TempDir(), "solovey-ui")
	member, err := writeExtractedReleaseMember(productionReleaseMemberOps, target, "solovey-ui", bytes.NewReader(data), int64(len(data)), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if reopened, err := os.ReadFile(target); err != nil || !bytes.Equal(reopened, data) || member.Digest != updateDigest(string(data)) {
		t.Fatalf("reopened=%q member=%#v err=%v", reopened, member, err)
	}
}

func TestPublicationPreparedGenerationFaultMatrixPublishesOnlyReopenableTrees(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	fault := errors.New("publication generation publication fault")
	tests := []struct {
		name          string
		publishedTree bool
	}{
		{"ensure-root", false},
		{"staging-create", false},
		{"extract", false},
		{"identity-write", false},
		{"profile-verify", false},
		{"tree-sync", false},
		{"final-rename", false},
		{"release-root-sync", true},
		{"final-reopen", true},
		{"final-tree-verify", true},
		{"final-member-verify", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			archive, identity := publicationCoreArchive(t, parent)
			host := NewHost()
			host.ReleaseRoot = filepath.Join(parent, "releases")
			host.generationOps = productionUpdateGenerationOps
			switch test.name {
			case "ensure-root":
				host.generationOps.ensureRoot = func(string, os.FileMode) error { return fault }
			case "staging-create":
				host.generationOps.mkdir = func(string, os.FileMode) error { return fault }
			case "extract":
				host.generationOps.extract = func(string, string) ([]extractedReleaseMember, error) { return nil, fault }
			case "identity-write":
				host.atomicOps = publicationFaultAtomicOps("write", fault)
			case "profile-verify":
				host.generationOps.verifyProfile = func(string, string) error { return fault }
			case "tree-sync":
				host.generationOps.syncTree = func(string) error { return fault }
			case "final-rename":
				host.generationOps.rename = func(string, string) error { return fault }
			case "release-root-sync":
				host.generationOps.syncDirectory = func(string) error { return fault }
			case "final-reopen":
				host.generationOps.loadIdentity = func(string) (ReleaseIdentityV1, error) { return ReleaseIdentityV1{}, fault }
			case "final-tree-verify":
				host.generationOps.verifyTree = func(string) error { return fault }
			case "final-member-verify":
				host.generationOps.verifyMembers = func(string, []extractedReleaseMember) error { return fault }
			}
			finalRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(identity))
			err := host.publishPreparedGeneration(archive, finalRoot, identity)
			if !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			_, statErr := os.Lstat(finalRoot)
			if got := statErr == nil; got != test.publishedTree {
				t.Fatalf("published tree=%v want=%v stat=%v", got, test.publishedTree, statErr)
			}
			if test.publishedTree {
				stored, loadErr := loadReleaseIdentity(finalRoot)
				if loadErr != nil || !sameReleaseIdentity(stored, identity, true) || verifyPreparedReleaseExecutables(finalRoot, "core") != nil || verifyReleaseTree(finalRoot) != nil {
					t.Fatalf("rename-visible generation is not reopenable: stored=%#v err=%v", stored, loadErr)
				}
			}
		})
	}
}

func TestPublicationPreparedGenerationSuccessReopensExactMemberIdentities(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	parent := t.TempDir()
	archive, identity := publicationCoreArchive(t, parent)
	host := NewHost()
	host.ReleaseRoot = filepath.Join(parent, "releases")
	finalRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(identity))
	if err := host.publishPreparedGeneration(archive, finalRoot, identity); err != nil {
		t.Fatal(err)
	}
	stored, err := loadReleaseIdentity(finalRoot)
	if err != nil || !sameReleaseIdentity(stored, identity, true) || verifyPreparedReleaseExecutables(finalRoot, "core") != nil || verifyReleaseTree(finalRoot) != nil {
		t.Fatalf("published generation did not reopen exactly: %#v %v", stored, err)
	}
}

func TestPublicationCoreOwnerContractRemovalIsDirectoryDurable(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "application-owner-contract.json")
	if err := os.WriteFile(path, []byte(`{"owner":"full"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	events := []string{}
	ops := updateRemovalOps{remove: func(name string) error {
		events = append(events, "remove")
		return os.Remove(name)
	}, syncDirectory: func(name string) error {
		events = append(events, "directory-sync:"+name)
		return syncDirectory(name)
	}}
	if err := removeUpdateAuthority(path, ops); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0] != "remove" || events[1] != "directory-sync:"+directory {
		t.Fatalf("events=%v", events)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner contract survived removal: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"owner":"full"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("owner directory sync fault")
	err := removeUpdateAuthority(path, updateRemovalOps{remove: os.Remove, syncDirectory: func(string) error { return fault }})
	if !errors.Is(err, fault) || !updateAuthorityVisible(err) {
		t.Fatalf("visible removal boundary=%v", err)
	}
}

func TestPublicationActivateAndRollbackExposeOneExactFullOrCoreGeneration(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	for _, transition := range []struct {
		name          string
		oldProfile    string
		targetProfile string
	}{
		{"full-to-core", "full", "core"},
		{"core-to-full", "core", "full"},
	} {
		t.Run(transition.name, func(t *testing.T) {
			root, err := os.MkdirTemp("/", "solovey-update-publication-generation-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			host := NewHost()
			host.ReleaseRoot = filepath.Join(root, "releases")
			stateDirectory := filepath.Join(root, "state")
			manifestDirectory := filepath.Join(root, "authority")
			ownerDirectory := filepath.Join(root, "owner")
			for _, directory := range []string{stateDirectory, manifestDirectory, ownerDirectory} {
				if err := os.Mkdir(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			host.StatePath = filepath.Join(stateDirectory, "update-state.json")
			host.Manifest = filepath.Join(manifestDirectory, "broker-clients.json")
			ownerPath := filepath.Join(ownerDirectory, "application-owner-contract.json")
			host.OwnerManifest = publicationOwnerPublisher(ownerPath)
			host.Restart = nil

			oldArchive, oldIdentity := publicationArchive(t, root, transition.oldProfile, 10)
			oldRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(oldIdentity))
			if err := host.publishPreparedGeneration(oldArchive, oldRoot, oldIdentity); err != nil {
				t.Fatal(err)
			}
			targetArchive, targetIdentity := publicationArchive(t, root, transition.targetProfile, 11)
			targetRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(targetIdentity))
			if err := host.publishPreparedGeneration(targetArchive, targetRoot, targetIdentity); err != nil {
				t.Fatal(err)
			}
			if err := host.activateLink(filepath.Base(oldRoot)); err != nil {
				t.Fatal(err)
			}
			seedManifest := publicationManifest(t, filepath.Join(oldRoot, "solovey-ui"))
			seedData, err := json.Marshal(seedManifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := publishUpdateAuthority(productionUpdateAtomicOps, host.Manifest, append(seedData, '\n'), 0o640, 0, 0, func() error {
				_, err := broker.LoadManifest(host.Manifest)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := host.OwnerManifest(context.Background(), oldRoot, transition.oldProfile); err != nil {
				t.Fatal(err)
			}
			operationID := "update-operation:publication-" + transition.name
			preparedRef := semanticRef("prepared", operationID, targetIdentity.ManifestDigest)
			rollbackRef := semanticRef("rollback", operationID, targetIdentity.ManifestDigest)
			initial := diskState{ActiveRelease: filepath.Base(oldRoot), ActiveSequence: oldIdentity.Sequence, ActiveDigest: oldIdentity.ManifestDigest,
				PreparedRollbackRelease: filepath.Base(oldRoot), PreparedRollbackSequence: oldIdentity.Sequence, PreparedRollbackDigest: oldIdentity.ManifestDigest,
				PreparedRollbackOperationID: operationID, PreparedRollbackManifestDigest: targetIdentity.ManifestDigest, PreparedRollbackRef: rollbackRef}
			if err := host.saveState(initial); err != nil {
				t.Fatal(err)
			}
			activatePayload, activateDigest, err := broker.MarshalPayload(ActivateRequestV1{Release: targetIdentity, PreparedRef: preparedRef,
				RollbackRef: rollbackRef, ExpectedMode: "native"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := host.activate(context.Background(), broker.Request{OperationID: operationID, Payload: activatePayload, PayloadDigest: activateDigest}, broker.PeerIdentity{PID: 42}); err != nil {
				t.Fatal(err)
			}
			assertPublicationAuthorityGeneration(t, host, ownerPath, targetIdentity)

			rollbackPayload, rollbackDigest, err := broker.MarshalPayload(RollbackRequestV1{Release: targetIdentity, RollbackRef: rollbackRef, ReasonCode: "publication-test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := host.rollback(context.Background(), broker.Request{OperationID: operationID, Payload: rollbackPayload, PayloadDigest: rollbackDigest}, broker.PeerIdentity{PID: 42}); err != nil {
				t.Fatal(err)
			}
			assertPublicationAuthorityGeneration(t, host, ownerPath, oldIdentity)
		})
	}
}

type publicationFaultAtomicFile struct {
	*os.File
	faultAt string
	fault   error
}

func (f *publicationFaultAtomicFile) Write(data []byte) (int, error) {
	if f.faultAt == "write" {
		return 0, f.fault
	}
	return f.File.Write(data)
}

func (f *publicationFaultAtomicFile) Sync() error {
	if f.faultAt == "sync" {
		return f.fault
	}
	return f.File.Sync()
}

func (f *publicationFaultAtomicFile) Close() error {
	err := f.File.Close()
	if f.faultAt == "close" && err == nil {
		return f.fault
	}
	return err
}

func publicationFaultAtomicOps(faultAt string, fault error) updateAtomicOps {
	ops := productionUpdateAtomicOps
	ops.createTemp = func(directory, pattern string) (updateAtomicFile, error) {
		if faultAt == "create" {
			return nil, fault
		}
		file, err := os.CreateTemp(directory, pattern)
		if err != nil {
			return nil, err
		}
		return &publicationFaultAtomicFile{File: file, faultAt: faultAt, fault: fault}, nil
	}
	if faultAt == "rename" {
		ops.rename = func(string, string) error { return fault }
	}
	if faultAt == "sync-dir" {
		ops.syncDirectory = func(string) error { return fault }
	}
	return ops
}

func publicationExecutable(t *testing.T, path, label string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n# "+label+"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func publicationManifest(t *testing.T, executable string) broker.Manifest {
	t.Helper()
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("executable stat identity unavailable")
	}
	digest, err := fileDigest(executable)
	if err != nil {
		t.Fatal(err)
	}
	entry := broker.ClientManifest{Name: "panel-legacy-root", UID: 0, GID: 0, Executable: executable, ExecutableDigest: digest,
		Device: uint64(stat.Dev), Inode: stat.Ino, CgroupUnit: "solovey-ui-native-legacy-root.service", CgroupPolicy: broker.CgroupRequired,
		CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1, Roles: []broker.Role{broker.RolePanel}}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{entry}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func publicationCoreArchive(t *testing.T, root string) (string, ReleaseIdentityV1) {
	t.Helper()
	return publicationArchive(t, root, "core", 10)
}

func publicationArchive(t *testing.T, root, profile string, sequence uint64) (string, ReleaseIdentityV1) {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	names := []string{"solovey-ui", "solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"}
	if profile == "full" {
		names = append(names, "solovey-owner-manifest")
	}
	for _, name := range names {
		content := []byte("#!/bin/sh\n# " + name + "\nexit 0\n")
		if err := tarWriter.WriteHeader(&tar.Header{Name: "solovey-ui/" + name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "solovey-ui-"+profile+"-"+string(rune('a'+sequence%20))+"-linux-amd64.tar.gz")
	if err := os.WriteFile(archive, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := ArtifactIdentityV1{Name: filepath.Base(archive), Role: "panel-" + profile, Platform: "linux", Arch: "amd64",
		MediaType: "application/gzip", Size: int64(compressed.Len()), SHA256: updateDigest(compressed.String()), Provenance: "release-ci"}
	identity := ReleaseIdentityV1{ReleaseID: "solovey-ui-main-" + string(rune('a'+sequence%20)), Sequence: sequence, Version: "2026.4.0", ManifestDigest: updateDigest("publication-manifest-" + profile + string(rune(sequence))),
		ArtifactSetDigest: artifactSetDigest([]ArtifactIdentityV1{artifact}), BinaryProfile: profile, DeploymentRevision: updateDigest("publication-deployment"),
		MigrationSetDigest: updateDigest("publication-migration"), RestartClass: "stack", RollbackClass: "automatic", Artifacts: []ArtifactIdentityV1{artifact}}
	return archive, identity
}

func publicationOwnerPublisher(path string) func(context.Context, string, string) error {
	return func(_ context.Context, releaseRoot, profile string) error {
		if profile == "core" {
			return removeUpdateAuthority(path, productionUpdateRemovalOps)
		}
		if profile != "full" {
			return errors.New("invalid owner profile")
		}
		data := []byte("full:" + filepath.Base(releaseRoot))
		return publishUpdateAuthority(productionUpdateAtomicOps, path, data, 0o600, 0, 0, func() error {
			reopened, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(reopened, data) {
				return errors.Join(err, errors.New("owner authority did not reopen exactly"))
			}
			return nil
		})
	}
}

func assertPublicationAuthorityGeneration(t *testing.T, host *Host, ownerPath string, identity ReleaseIdentityV1) {
	t.Helper()
	name := releaseDirectoryName(identity)
	state, err := host.loadState()
	if err != nil || state.ActiveRelease != name || state.ActiveSequence != identity.Sequence || state.ActiveDigest != identity.ManifestDigest {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	current, err := os.Readlink(filepath.Join(host.ReleaseRoot, "current"))
	if err != nil || current != name {
		t.Fatalf("current=%q err=%v", current, err)
	}
	releaseRoot := filepath.Join(host.ReleaseRoot, name)
	stored, err := loadReleaseIdentity(releaseRoot)
	if err != nil || !sameReleaseIdentity(stored, identity, true) || verifyPreparedReleaseExecutables(releaseRoot, identity.BinaryProfile) != nil || verifyReleaseTree(releaseRoot) != nil {
		t.Fatalf("release authority=%#v err=%v", stored, err)
	}
	manifest, err := broker.LoadManifest(host.Manifest)
	if err != nil || len(manifest.Clients) != 1 || manifest.Clients[0].Executable != filepath.Join(releaseRoot, "solovey-ui") {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	owner, err := os.ReadFile(ownerPath)
	if identity.BinaryProfile == "core" {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("core owner authority=%q err=%v", owner, err)
		}
	} else if err != nil || string(owner) != "full:"+name {
		t.Fatalf("full owner authority=%q err=%v", owner, err)
	}
}

func requireRootUpdateBrokerTest(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned update authority fixture")
	}
}
