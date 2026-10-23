package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type panelArchiveFixture struct {
	name, body, link string
	kind             byte
	size             int64
}

func panelTarFixture(t *testing.T, entries []panelArchiveFixture, complete bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		size := int64(len(entry.body))
		if entry.size != 0 {
			size = entry.size
		}
		header := &tar.Header{Name: entry.name, Typeflag: entry.kind, Linkname: entry.link, Size: size, Mode: 0o755, Format: tar.FormatGNU}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if len(entry.body) > 0 {
			if _, err := io.WriteString(writer, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Incomplete fixtures intentionally omit the tar close/end records.
	if complete {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buffer.Bytes()
}

func panelGzipFixture(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func readPanelArchiveFixture(raw []byte) (map[string]string, error) {
	reader, err := NewPanelArchiveReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	result := map[string]string{}
	for {
		member, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		if int64(len(body)) != member.Size {
			return nil, errors.New("fixture content differs from member size")
		}
		result[member.Relative] = string(body)
	}
}

func TestPanelArchiveGrammarAcceptsCanonicalDirectoriesAndBoundedPadding(t *testing.T) {
	entries := []panelArchiveFixture{
		{name: "solovey-ui/", kind: tar.TypeDir}, {name: "solovey-ui/systemd/", kind: tar.TypeDir},
		{name: "solovey-ui/solovey-ui", body: "panel-fixture", kind: tar.TypeReg},
		{name: "solovey-ui/systemd/solovey-ui-native-hardened.service", body: "unit-fixture", kind: tar.TypeReg},
	}
	for _, padding := range []int{0, 4096, 10240} {
		raw := append(panelTarFixture(t, entries, true), make([]byte, padding)...)
		members, err := readPanelArchiveFixture(panelGzipFixture(t, raw))
		if err != nil || len(members) != 2 || members["solovey-ui"] != "panel-fixture" || members["systemd/solovey-ui-native-hardened.service"] != "unit-fixture" {
			t.Fatalf("canonical archive at padding %d: members=%v err=%v", padding, members, err)
		}
	}
}

func TestPanelArchiveRejectsAmbiguousAndUnsafeMembers(t *testing.T) {
	file := panelArchiveFixture{name: "solovey-ui/solovey-ui", body: "panel", kind: tar.TypeReg}
	for name, entries := range map[string][]panelArchiveFixture{
		"duplicate-file":      {file, file},
		"duplicate-directory": {{name: "solovey-ui/", kind: tar.TypeDir}, {name: "solovey-ui/", kind: tar.TypeDir}, file},
		"linked-directory":    {{name: "solovey-ui/systemd/", kind: tar.TypeDir, link: "credential-sentinel"}, file},
		"directory-only":      {{name: "solovey-ui/", kind: tar.TypeDir}},
		"unknown-directory":   {{name: "solovey-ui/db/", kind: tar.TypeDir}, file},
		"traversal":           {{name: "solovey-ui/../credential-sentinel", kind: tar.TypeReg}},
		"absolute":            {{name: "/credential-sentinel", kind: tar.TypeReg}},
		"backslash":           {{name: "solovey-ui/systemd\\credential-sentinel", kind: tar.TypeReg}},
		"second-root":         {{name: "other/credential-sentinel", kind: tar.TypeReg}},
		"nested":              {{name: "solovey-ui/systemd/nested/credential-sentinel", kind: tar.TypeReg}},
		"repeated-slash":      {{name: "solovey-ui/systemd//credential-sentinel", kind: tar.TypeReg}},
		"symlink":             {{name: "solovey-ui/link", kind: tar.TypeSymlink, link: "credential-sentinel"}},
		"hardlink":            {{name: "solovey-ui/link", kind: tar.TypeLink, link: "solovey-ui/solovey-ui"}},
		"fifo":                {{name: "solovey-ui/pipe", kind: tar.TypeFifo}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readPanelArchiveFixture(panelGzipFixture(t, panelTarFixture(t, entries, true)))
			if !errors.Is(err, ErrPanelArchive) || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatalf("unsafe archive accepted or echoed: %v", err)
			}
		})
	}
	for _, count := range []int{MaxPanelArchiveEntries, MaxPanelArchiveEntries + 1} {
		entries := make([]panelArchiveFixture, count)
		for i := range entries {
			entries[i] = panelArchiveFixture{name: fmt.Sprintf("solovey-ui/file-%d", i), kind: tar.TypeReg}
		}
		members, err := readPanelArchiveFixture(panelGzipFixture(t, panelTarFixture(t, entries, true)))
		if count == MaxPanelArchiveEntries && (err != nil || len(members) != count) {
			t.Fatal("boundary inventory rejected", err)
		}
		if count > MaxPanelArchiveEntries && !errors.Is(err, ErrPanelArchive) {
			t.Fatal("oversized inventory accepted", err)
		}
	}
}

func TestPanelArchiveValidatesCompleteGzipAndRejectsTrailingPayloads(t *testing.T) {
	file := panelArchiveFixture{name: "solovey-ui/solovey-ui", body: "panel", kind: tar.TypeReg}
	tarBytes := panelTarFixture(t, []panelArchiveFixture{file}, true)
	valid := panelGzipFixture(t, tarBytes)
	badChecksum := bytes.Clone(valid)
	badChecksum[len(badChecksum)-8] ^= 1
	for name, raw := range map[string][]byte{
		"empty": {}, "empty-gzip": panelGzipFixture(t, nil), "empty-tar": panelGzipFixture(t, make([]byte, 1024)),
		"checksum": badChecksum, "truncated-gzip": valid[:len(valid)-4],
		"nonzero-tail":     panelGzipFixture(t, append(bytes.Clone(tarBytes), []byte("credential-sentinel")...)),
		"second-tar":       panelGzipFixture(t, append(bytes.Clone(tarBytes), tarBytes...)),
		"member-size":      panelGzipFixture(t, panelTarFixture(t, []panelArchiveFixture{{name: file.name, kind: tar.TypeReg, size: MaxPanelMemberBytes + 1}}, false)),
		"truncated-member": panelGzipFixture(t, panelTarFixture(t, []panelArchiveFixture{{name: file.name, kind: tar.TypeReg, size: 1024}}, false)),
		"no-end-records":   panelGzipFixture(t, tarBytes[:len(tarBytes)-1024]),
		"one-end-record":   panelGzipFixture(t, tarBytes[:len(tarBytes)-512]),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readPanelArchiveFixture(raw)
			if !errors.Is(err, ErrPanelArchive) || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatalf("incomplete/ambiguous archive accepted or echoed: %v", err)
			}
		})
	}
}
