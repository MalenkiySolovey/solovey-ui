//go:build linux

package updatebroker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerDiskCheckpointRequiresTheSameNamedBoundPredecessor(t *testing.T) {
	state := providerCheckpointState(t, providerReleaseFixture(t, "shtorm-extended"), providerMaintenanceFixture(t))
	state.ActiveRelease = "installer-" + updateDigest("generation")[:16]
	state.ActiveSequence, state.ActiveDigest = 0, ""
	state.ActiveLocalCheckpoint = LocalGenerationCheckpointV1{Kind: installerRootCheckpoint, Digest: updateDigest("contents")}
	state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest = state.ActiveRelease, 0, ""
	state.PreparedRollbackLocalCheckpoint = state.ActiveLocalCheckpoint
	if err := validateDiskState(state); err != nil {
		t.Fatal("bound installer checkpoint was rejected", err)
	}
	for _, mutate := range []func(*diskState){
		func(v *diskState) { v.ActiveLocalCheckpoint.Digest = updateDigest("different predecessor") },
		func(v *diskState) { v.PreparedRollbackLocalCheckpoint = LocalGenerationCheckpointV1{} },
		func(v *diskState) { v.PreparedRollbackRelease = "00000000000000000018-1111111111111111" },
		func(v *diskState) { v.PreparedRollbackOperationID = "" },
		func(v *diskState) { v.ActiveLocalCheckpoint.Kind = "unknown/v1" },
	} {
		changed := state
		mutate(&changed)
		if validateDiskState(changed) == nil {
			t.Fatal("mixed or unbound installer checkpoint was accepted")
		}
	}
	state.ProviderMaintenance.Phase = "ROLLED_BACK"
	if err := validateDiskState(state); err != nil {
		t.Fatal("restored installer checkpoint was rejected", err)
	}
	state.ActiveLocalCheckpoint.Digest = updateDigest("unrelated restored contents")
	if validateDiskState(state) == nil {
		t.Fatal("unrelated restored local contents retained rollback authority")
	}
}

func TestInstallerCheckpointRejectsUnsafeAndAmbiguousGeneration(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	host := NewHost()
	host.ReleaseRoot = t.TempDir()
	name := "installer-" + updateDigest("installer generation")[:16]
	root := filepath.Join(host.ReleaseRoot, name)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"solovey-ui", "solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"} {
		publicationExecutable(t, filepath.Join(root, file), "local checkpoint fixture")
	}
	metadata := filepath.Join(root, "BUILD_INFO.txt")
	if err := os.WriteFile(metadata, []byte("profile=core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := host.installerCheckpoint(name)
	if err != nil || !host.localCheckpointMatches(name, local) {
		t.Fatal("Root-owned installer generation was not bound", err)
	}
	for _, mode := range []os.FileMode{0o666, 0o664} {
		if err := os.Chmod(metadata, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := host.installerCheckpoint(name); err == nil {
			t.Fatal("writable installer contents qualified as a rollback checkpoint")
		}
	}
	if err := os.Chmod(metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "solovey-ui.sh")
	if err := os.Symlink(metadata, link); err != nil {
		t.Fatal(err)
	}
	if _, err := host.installerCheckpoint(name); err == nil {
		t.Fatal("symlinked installer contents qualified as a rollback checkpoint")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(root, "release-identity.json")
	if err := os.WriteFile(identity, []byte("malformed signed identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.installerCheckpoint(name); err == nil {
		t.Fatal("malformed signed metadata fell back to local checkpoint authority")
	}
	if err := os.Remove(identity); err != nil {
		t.Fatal(err)
	}
	qualified := releaseDirectoryName(validHostReleaseIdentity())
	if err := os.Rename(root, filepath.Join(host.ReleaseRoot, qualified)); err != nil {
		t.Fatal(err)
	}
	if _, err := host.installerCheckpoint(qualified); err == nil {
		t.Fatal("a missing signed identity fell back outside the installer namespace")
	}
}
