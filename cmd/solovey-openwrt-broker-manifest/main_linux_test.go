//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

func TestOpenWrtManifestWriterUsesCoherentReleaseObjectTuple(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned release object is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-openwrt-manifest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "solovey-ui")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o555)
	if err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeInputErr, closeOutputErr := input.Close(), output.Close()
	if copyErr != nil || closeInputErr != nil || closeOutputErr != nil {
		t.Fatal("release fixture copy failed")
	}
	entry, err := executable(path)
	if err != nil {
		t.Fatal(err)
	}
	object, err := executableobject.Open(path, executableobject.Policy{MaxBytes: maxClientExecutableBytes,
		RequireRegular: true, RequireExecutable: true, RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	identity := object.Identity()
	if entry.Path != path || entry.SHA256 != identity.Digest || entry.Device != identity.Device || entry.Inode != identity.Inode {
		t.Fatalf("entry=%+v identity=%+v", entry, identity)
	}
}
