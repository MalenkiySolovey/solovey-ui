package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func updateArchive(t *testing.T, entry string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: entry, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func updateArtifactServer(t *testing.T, archive []byte, checksum string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write(archive) })
	mux.HandleFunc("/checksum", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(checksum + "  solovey-ui-linux-amd64.tar.gz\n"))
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestApplyPipelineVerifiesBeforeReplacing(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "solovey-ui")
	if err := os.WriteFile(executable, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := updateArchive(t, "solovey-ui/solovey-ui", []byte("NEW"))
	server := updateArtifactServer(t, archive, strings.Repeat("0", 64))
	target := ReleaseTarget{Version: "9999.0.0", AssetURL: server.URL + "/asset", ChecksumURL: server.URL + "/checksum"}
	if err := ApplyPipeline(target, PipelineDeps{Client: server.Client(), ExecPath: executable}, func(UpdateStage) {}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v", err)
	}
	if content, _ := os.ReadFile(executable); string(content) != "OLD" {
		t.Fatalf("live binary changed to %q", content)
	}
	if _, err := os.Stat(executable + backupSuffix); !os.IsNotExist(err) {
		t.Fatal("backup created before checksum verification")
	}
}

func TestApplyPipelineReplacesBinaryAndKeepsBackup(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "solovey-ui")
	if err := os.WriteFile(executable, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := updateArchive(t, "solovey-ui/solovey-ui", []byte("NEW"))
	sum := sha256.Sum256(archive)
	server := updateArtifactServer(t, archive, hex.EncodeToString(sum[:]))
	target := ReleaseTarget{Version: "9999.0.0", AssetURL: server.URL + "/asset", ChecksumURL: server.URL + "/checksum"}
	if err := ApplyPipeline(target, PipelineDeps{Client: server.Client(), ExecPath: executable}, func(UpdateStage) {}); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(executable); string(content) != "NEW" {
		t.Fatalf("live binary = %q", content)
	}
	if content, _ := os.ReadFile(executable + backupSuffix); string(content) != "OLD" {
		t.Fatalf("backup = %q", content)
	}
}

func TestExtractBinaryRequiresExactReleasePath(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "bad.tar.gz")
	if err := os.WriteFile(archivePath, updateArchive(t, "nested/solovey-ui", []byte("BAD")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archivePath, filepath.Join(directory, "out")); err == nil {
		t.Fatal("unexpected archive path accepted")
	}
}

func TestCheckPendingRollsBackAfterThreshold(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "solovey-ui")
	_ = os.WriteFile(executable, []byte("BROKEN"), 0o755)
	_ = os.WriteFile(executable+backupSuffix, []byte("GOOD"), 0o755)
	if err := writePendingMarker(executable); err != nil {
		t.Fatal(err)
	}
	if CheckPending(executable) {
		t.Fatal("rolled back on first boot")
	}
	if !CheckPending(executable) {
		t.Fatal("did not roll back on second failed boot")
	}
	if content, _ := os.ReadFile(executable); string(content) != "GOOD" {
		t.Fatalf("restored binary = %q", content)
	}
}

func TestCheckPendingResetsInvalidMarker(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "solovey-ui")
	if err := os.WriteFile(executable+pendingSuffix, []byte("not-a-number"), 0o600); err != nil {
		t.Fatal(err)
	}

	if CheckPending(executable) {
		t.Fatal("invalid marker should reset attempts without rollback")
	}

	got, err := os.ReadFile(executable + pendingSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1" {
		t.Fatalf("pending marker = %q, want 1", got)
	}
}
