//go:build linux

package updatebroker

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseExtractionAcceptsCanonicalPackageDirectories(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "release.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)
	for _, entry := range []struct {
		name, body string
		kind       byte
	}{
		{"solovey-ui/", "", tar.TypeDir},
		{"solovey-ui/systemd/", "", tar.TypeDir},
		{"solovey-ui/solovey-ui", "panel-fixture", tar.TypeReg},
		{"solovey-ui/systemd/solovey-ui-native-hardened.service", "unit-fixture", tar.TypeReg},
	} {
		if err := writer.WriteHeader(&tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: 0o755, Size: int64(len(entry.body)), Format: tar.FormatGNU}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{writer, compressed, file} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(root, "staging")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	members, err := extractRelease(archive, destination)
	if err != nil || len(members) != 2 {
		t.Fatalf("canonical package extraction: members=%d err=%v", len(members), err)
	}
	for path, expected := range map[string]string{"solovey-ui": "panel-fixture", "systemd/solovey-ui-native-hardened.service": "unit-fixture"} {
		body, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(path)))
		if err != nil || string(body) != expected {
			t.Fatalf("extracted member %s differs: %v", path, err)
		}
	}
}
