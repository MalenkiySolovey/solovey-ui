//go:build linux

package mountevidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestObservePreservesLogicalTargetAndResolvedFilesystemTarget(t *testing.T) {
	root := t.TempDir()
	canonicalParent := filepath.Join(root, "canonical")
	canonicalTarget := filepath.Join(canonicalParent, "runtime")
	if err := os.MkdirAll(canonicalTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	logicalParent := filepath.Join(root, "logical")
	if err := os.Symlink(canonicalParent, logicalParent); err != nil {
		t.Fatal(err)
	}
	logicalTarget := filepath.ToSlash(filepath.Join(logicalParent, "runtime"))
	fact, err := Observe(logicalTarget)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Target != logicalTarget || fact.ResolvedTarget != filepath.ToSlash(canonicalTarget) || fact.Validate() != nil {
		t.Fatalf("logical/canonical mount fact = %#v", fact)
	}
}
