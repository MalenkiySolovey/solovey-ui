//go:build linux

package updatebroker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGenerationPublicationPreservesModesWithBrokerUmask(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	const child = "SOLOVEY_UPDATE_UMASK_TEST_CHILD"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGenerationPublicationPreservesModesWithBrokerUmask$", "-test.count=1")
		command.Env = append(os.Environ(), child+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("broker umask subprocess failed: %v\n%s", err, output)
		}
		return
	}
	// This process runs only this test; the test parent and other tests retain
	// their own umask. Match the installed privileged broker's UMask=0077.
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	parent := t.TempDir()
	archive, identity := publicationCoreArchive(t, parent)
	host := NewHost()
	host.ReleaseRoot = filepath.Join(parent, "releases")
	finalRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(identity))
	if err := host.publishPreparedGeneration(archive, finalRoot, identity); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{host.ReleaseRoot, finalRoot, filepath.Join(finalRoot, "solovey-ui"),
		filepath.Join(finalRoot, "solovey-privileged-broker"), filepath.Join(finalRoot, "solovey-ssh-proof"), filepath.Join(finalRoot, "solovey-broker-manifest")} {
		assertGenerationMode(t, path, 0o755)
	}
	assertGenerationMode(t, filepath.Join(finalRoot, "release-identity.json"), 0o600)

	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	const body = "public unit metadata"
	if err := tarWriter.WriteHeader(&tar.Header{Name: "solovey-ui/systemd/fixture.service", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []interface{ Close() error }{tarWriter, gzipWriter} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	metadataArchive := filepath.Join(parent, "metadata.tar.gz")
	if err := os.WriteFile(metadataArchive, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractRelease(metadataArchive, finalRoot); err != nil {
		t.Fatal(err)
	}
	assertGenerationMode(t, filepath.Join(finalRoot, "systemd"), 0o755)
	assertGenerationMode(t, filepath.Join(finalRoot, "systemd", "fixture.service"), 0o644)
}

func assertGenerationMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != expected || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Fatalf("published mode for %s differs from %o: %v %v", filepath.Base(path), expected, info, err)
	}
}
