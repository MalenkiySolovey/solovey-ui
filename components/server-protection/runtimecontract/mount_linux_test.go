//go:build linux

package runtimecontract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func TestRuntimeMountProofAcceptsDirectVolatileRoot(t *testing.T) {
	logicalRoot := newRuntimeRoot(t, newVolatileRoot(t), "solovey-ui", "server-protection")
	proof, err := ObserveRuntimeMount(logicalRoot, RuntimeMountVolatile)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Root != logicalRoot || proof.Mount.Target != logicalRoot || proof.Mount.ResolvedTarget != logicalRoot || proof.Validate() != nil {
		t.Fatalf("direct runtime root proof = %#v", proof)
	}
}

func TestRuntimeMountProofAcceptsSingleAncestorSymlink(t *testing.T) {
	fixtureRoot := t.TempDir()
	volatileRoot := newVolatileRoot(t)
	canonicalRoot := newRuntimeRoot(t, volatileRoot, "run", "solovey-ui", "server-protection")
	if err := os.Symlink(filepath.Join(volatileRoot, "run"), filepath.Join(fixtureRoot, "run")); err != nil {
		t.Fatal(err)
	}
	assertLogicalCanonicalRuntimeProof(t,
		filepath.Join(fixtureRoot, "run", "solovey-ui", "server-protection"), canonicalRoot)
}

func TestRuntimeMountProofPreservesLogicalAndCanonicalRootIdentity(t *testing.T) {
	volatileRoot := newVolatileRoot(t)
	canonicalRoot := newRuntimeRoot(t, volatileRoot, "run", "solovey-ui", "server-protection")
	fixtureRoot := t.TempDir()
	if err := os.Symlink(volatileRoot, filepath.Join(fixtureRoot, "var")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("var", "run"), filepath.Join(fixtureRoot, "run")); err != nil {
		t.Fatal(err)
	}

	logicalRoot := filepath.ToSlash(filepath.Join(fixtureRoot, "run", "solovey-ui", "server-protection"))
	assertLogicalCanonicalRuntimeProof(t, logicalRoot, canonicalRoot)
}

func TestRuntimeMountProofRejectsPersistentRedirectForVolatilePolicy(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	persistentRoot, err := os.MkdirTemp(home, "solovey-persistent-redirect-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(persistentRoot) })
	newRuntimeRoot(t, persistentRoot, "run", "solovey-ui", "server-protection")
	fixtureRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(persistentRoot, "run"), filepath.Join(fixtureRoot, "run")); err != nil {
		t.Fatal(err)
	}
	logicalRoot := filepath.ToSlash(filepath.Join(fixtureRoot, "run", "solovey-ui", "server-protection"))
	if _, err := ObserveRuntimeMount(logicalRoot, RuntimeMountVolatile); err == nil {
		t.Fatal("persistent runtime redirect was accepted as volatile authority")
	}
}

func TestRuntimeMountProofRejectsOwnerLocalSymlinkRedirect(t *testing.T) {
	volatileRoot := newVolatileRoot(t)
	unrelated := newRuntimeRoot(t, volatileRoot, "unrelated")
	logicalParent := filepath.Join(t.TempDir(), "run", "solovey-ui")
	if err := os.MkdirAll(logicalParent, 0o700); err != nil {
		t.Fatal(err)
	}
	logicalRoot := filepath.Join(logicalParent, "server-protection")
	if err := os.Symlink(unrelated, logicalRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveRuntimeMount(filepath.ToSlash(logicalRoot), RuntimeMountVolatile); err == nil {
		t.Fatal("owner-local runtime-root symlink was accepted")
	}
}

func TestRuntimeMountProofRejectsUnrelatedCanonicalNamespace(t *testing.T) {
	if sameOwnerLocalRoot("/run/solovey-ui/server-protection", "/volatile/unrelated") {
		t.Fatal("unrelated canonical namespace was accepted")
	}
	if !sameOwnerLocalRoot("/run/solovey-ui/server-protection", "/tmp/run/solovey-ui/server-protection") {
		t.Fatal("ancestor-only canonicalization was rejected")
	}
}

func TestRuntimeMountProofFencesAncestorSymlinkRetarget(t *testing.T) {
	first := newVolatileRoot(t)
	second := newVolatileRoot(t)
	newRuntimeRoot(t, first, "run", "solovey-ui", "server-protection")
	newRuntimeRoot(t, second, "run", "solovey-ui", "server-protection")
	fixtureRoot := t.TempDir()
	link := filepath.Join(fixtureRoot, "var")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("var", "run"), filepath.Join(fixtureRoot, "run")); err != nil {
		t.Fatal(err)
	}
	logicalRoot := filepath.ToSlash(filepath.Join(fixtureRoot, "run", "solovey-ui", "server-protection"))
	proof, err := ObserveRuntimeMount(logicalRoot, RuntimeMountVolatile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if err := recheckRuntimeMount(proof); err == nil {
		t.Fatal("ancestor symlink retarget was not fenced")
	}
}

func TestRuntimeMountProofFencesMountIdentityChange(t *testing.T) {
	logicalRoot := newRuntimeRoot(t, newVolatileRoot(t), "solovey-ui", "server-protection")
	proof, err := ObserveRuntimeMount(logicalRoot, RuntimeMountVolatile)
	if err != nil {
		t.Fatal(err)
	}
	changed := proof.Mount
	changed.MountID++
	if err := changed.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := recheckRuntimeMountWithObserver(proof, func(string) (mountevidence.Fact, error) {
		return changed, nil
	}); err == nil {
		t.Fatal("mount identity change was not fenced")
	}
}

func TestRuntimeMountProofAcceptsNonSymlinkedPersistentLinuxRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	persistentRoot, err := os.MkdirTemp(home, "solovey-persistent-root-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(persistentRoot) })
	logicalRoot := newRuntimeRoot(t, persistentRoot, "solovey-ui", ".runtime", "server-protection")
	proof, err := ObserveRuntimeMount(logicalRoot, RuntimeMountPersistent)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Mount.Target != logicalRoot || proof.Mount.ResolvedTarget != logicalRoot || proof.Validate() != nil {
		t.Fatalf("non-symlinked persistent runtime root proof = %#v", proof)
	}
}

func assertLogicalCanonicalRuntimeProof(t testing.TB, logicalRoot, canonicalRoot string) {
	t.Helper()
	logicalRoot = filepath.ToSlash(filepath.Clean(logicalRoot))
	canonicalRoot = filepath.ToSlash(filepath.Clean(canonicalRoot))
	proof, err := ObserveRuntimeMount(logicalRoot, RuntimeMountVolatile)
	if err != nil {
		t.Fatalf("observe logical runtime root: %v", err)
	}
	if proof.Root != logicalRoot || proof.Mount.Target != logicalRoot || proof.Mount.ResolvedTarget != canonicalRoot {
		t.Fatalf("logical/canonical runtime root identity = root %q target %q resolved %q, want %q %q %q",
			proof.Root, proof.Mount.Target, proof.Mount.ResolvedTarget, logicalRoot, logicalRoot, canonicalRoot)
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("validate logical runtime root on its canonical volatile filesystem: %v", err)
	}
}

func newVolatileRoot(t testing.TB) string {
	t.Helper()
	root, err := os.MkdirTemp("/dev/shm", "solovey-runtime-root-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func newRuntimeRoot(t testing.TB, base string, elements ...string) string {
	t.Helper()
	parts := append([]string{base}, elements...)
	root := filepath.Join(parts...)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Clean(root))
}
