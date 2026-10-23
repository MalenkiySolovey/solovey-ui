package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

type observedCandidate struct {
	Schema        string                     `json:"schema"`
	ContentDigest string                     `json:"contentDigest"`
	Size          int64                      `json:"size"`
	Build         release.PanelBuildIdentity `json:"build"`
	Members       []archiveMemberObservation `json:"members"`
}

// The actual full/core binaries carry dependency metadata absent from Go test
// executables. Both provider CI jobs supply each binary to this required probe.
func TestArtifactObservationActualBinaryAndArchive(t *testing.T) {
	binary := os.Getenv("SOLOVEY_ARTIFACT_TEST_BINARY")
	if binary == "" {
		t.Skip("actual full/core binary probe runs after the CI build")
	}
	id, profile := os.Getenv("SOLOVEY_ARTIFACT_TEST_PROVIDER"), os.Getenv("SOLOVEY_ARTIFACT_TEST_PROFILE")
	if (id != "sing-box" && id != "shtorm-extended") || (profile != "full" && profile != "core") {
		t.Fatal("actual binary probe requires explicit provider and profile")
	}
	source := providerfixture.Identity(t, id).Source
	rawSource, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(key, private)
	}
	observe := func(args []string, declaration []byte) (observedCandidate, error) {
		t.Helper()
		var output bytes.Buffer
		err := run(args, bytes.NewReader(declaration), &output)
		if err != nil {
			if output.Len() != 0 || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatal("failed archive emitted an observation or unsafe diagnostic")
			}
			return observedCandidate{}, err
		}
		var result observedCandidate
		if json.Unmarshal(output.Bytes(), &result) != nil {
			t.Fatal("observation is not valid JSON")
		}
		return result, nil
	}
	direct, err := observe([]string{binary}, rawSource)
	if err != nil || direct.Schema != "solovey.panel-artifact-observation/v1" || direct.Build.CoreProvider != source ||
		direct.Build.BinaryProfile != profile || len(direct.Members) != 0 {
		t.Fatal("actual binary did not produce its exact declared identity", err)
	}
	digest, size := candidateFixtureDigest(t, binary)
	if direct.ContentDigest != digest || direct.Size != size {
		t.Fatal("direct observation lost exact binary bytes")
	}
	// A misleading label must never select the provider. Payload text is merely
	// a hashed member; it cannot override the enclosed executable's metadata.
	archive := filepath.Join(root, "official-looking-credential-sentinel.tar.gz")
	writeActualPanelArchiveFixture(t, archive, binary, true)
	packaged, err := observe([]string{"--archive", archive}, rawSource)
	if err != nil || packaged.Schema != "solovey.panel-archive-observation/v1" || !reflect.DeepEqual(packaged.Build, direct.Build) || len(packaged.Members) != 3 {
		t.Fatal("canonical archive lost its actual binary identity", err)
	}
	archiveDigest, archiveSize := candidateFixtureDigest(t, archive)
	if packaged.ContentDigest != archiveDigest || packaged.Size != archiveSize ||
		packaged.Members[1] != (archiveMemberObservation{"solovey-ui", size, digest}) {
		t.Fatal("archive/member byte identities differ from the observed files")
	}
	t.Logf("provider=%s profile=%s binary_sha256=%s archive_sha256=%s members=%d", id, profile, digest, archiveDigest, len(packaged.Members))
	t.Run("wrong-source", func(t *testing.T) {
		other := "sing-box"
		if id == other {
			other = "shtorm-extended"
		}
		wrong, marshalErr := json.Marshal(providerfixture.Identity(t, other).Source)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err := observe([]string{"--archive", archive}, wrong); err == nil {
			t.Fatal("archive filename or text overrode the actual provider")
		}
	})
	t.Run("bad-checksum-after-panel", func(t *testing.T) {
		file, err := os.OpenFile(archive, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		var trailer [1]byte
		if _, err := file.ReadAt(trailer[:], archiveSize-8); err != nil {
			t.Fatal(err)
		}
		trailer[0] ^= 1
		if _, err := file.WriteAt(trailer[:], archiveSize-8); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := observe([]string{"--archive", archive}, rawSource); err == nil {
			t.Fatal("valid executable bypassed the final gzip checksum")
		}
	})
	t.Run("missing-tar-end-after-panel", func(t *testing.T) {
		incomplete := filepath.Join(root, "incomplete.tar.gz")
		writeActualPanelArchiveFixture(t, incomplete, binary, false)
		if _, err := observe([]string{"--archive", incomplete}, rawSource); err == nil {
			t.Fatal("valid executable bypassed a missing tar terminator")
		}
	})
	entries, err := os.ReadDir(private)
	if err != nil || len(entries) != 0 {
		t.Fatal("candidate spool was not removed after success or failure", err)
	}
}

func candidateFixtureDigest(t *testing.T, name string) (string, int64) {
	t.Helper()
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil)), size
}

func writeActualPanelArchiveFixture(t *testing.T, name, binary string, complete bool) {
	t.Helper()
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)
	for _, name := range []string{"solovey-ui/", "solovey-ui/systemd/"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755, Format: tar.FormatGNU}); err != nil {
			t.Fatal(err)
		}
	}
	panel, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = panel.Close() }()
	info, err := panel.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "solovey-ui/solovey-ui", Typeflag: tar.TypeReg, Mode: 0o755, Size: info.Size(), Format: tar.FormatGNU}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(writer, panel); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"solovey-ui/BUILD_INFO.txt", "solovey-ui/systemd/fixture.service"} {
		const text = "untrusted credential-sentinel metadata"
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(text)), Format: tar.FormatGNU}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, text); err != nil {
			t.Fatal(err)
		}
	}
	if complete {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else if err := writer.Flush(); err != nil { // Deliberately omit tar end records.
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
}
