//go:build !minimal

package zapret

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/gin-gonic/gin"
)

func TestPortablePresetEnvelopeIncludesDependencyIDsAtArchiveLimit(t *testing.T) {
	input := strategy.PresetInput{Format: "zip", Name: "Bounded portable preset"}
	for i := range 14 {
		input.DependencyIDs = append(input.DependencyIDs, strategy.PresetDependencyID{
			Path: fmt.Sprintf("lists/%d-%s.txt", i, strings.Repeat("long-", 36)),
			ID:   fmt.Sprintf("%02d-%s", i, strings.Repeat("l", 61)),
		})
	}
	archive := func(comment string) []byte {
		var data bytes.Buffer
		out := zip.NewWriter(&data)
		add := func(name, text, comment string) {
			t.Helper()
			file, err := out.CreateHeader(&zip.FileHeader{Name: name, Comment: comment, Method: zip.Store})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write([]byte(text)); err != nil {
				t.Fatal(err)
			}
		}
		var source strings.Builder
		var lists []map[string]string
		for i, override := range input.DependencyIDs {
			path := override.Path
			name := strings.TrimPrefix(path, "lists/")
			content := fmt.Sprintf("192.0.2.%d/32\n", i+1)
			add(path, content, strings.Repeat("x", 52<<10))
			lists = append(lists, map[string]string{"file_name": name, "archive_path": path, "kind": "ipset", "storage": "standalone", "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(content)))})
			if i > 0 {
				source.WriteString("--new\n")
			}
			fmt.Fprintf(&source, "--name=p-%d\n--filter-tcp=%d\n--ipset=%s\n", i, 18080+i, path)
		}
		manifest, err := json.Marshal(map[string]any{"format": "zapretgui-portable-preset", "version": 1, "engine": "winws2", "preset_file_name": "portable.txt", "lists": lists})
		if err != nil {
			t.Fatal(err)
		}
		add("manifest.json", string(manifest), "")
		add("preset.txt", source.String(), "")
		if err := out.SetComment(comment); err != nil {
			t.Fatal(err)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	input.Content = archive(strings.Repeat("x", strategy.MaxPresetTransferBytes-len(archive(""))))
	catalog := strategy.Catalog{Schema: strategy.CatalogSchema, Records: map[string]strategy.Record{}}
	if _, preview, err := catalog.PreparePreset("portable", input); err != nil || preview.Record == nil {
		t.Fatalf("at-limit archive must be admissible before HTTP decoding: %v, %+v", err, preview.Issues)
	}
	request := struct {
		ExpectedRevision string               `json:"expectedRevision"`
		Preset           strategy.PresetInput `json:"preset"`
	}{catalog.Revision(), input}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("archive=%d HTTP JSON=%d HTTP limit=%d", len(input.Content), len(data), maxPresetEnvelope)
	decode := func(body []byte) (strategy.PresetInput, error) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		decoded := request
		decoded.Preset = strategy.PresetInput{}
		err := readRequestLimit(ctx, &decoded, maxPresetEnvelope)
		return decoded.Preset, err
	}
	decoded, err := decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Content, input.Content) || !slices.Equal(decoded.DependencyIDs, input.DependencyIDs) {
		t.Fatal("the archive and ID overrides must reach the strategy importer intact")
	}
	if len(data) <= maxEditorEnvelope {
		t.Fatal("fixture must exceed the ordinary editor envelope")
	}
	oversized := append(data, strings.Repeat(" ", maxPresetEnvelope-len(data)+1)...)
	if _, err := decode(oversized); err == nil || !strings.Contains(err.Error(), "transfer limit") {
		t.Fatalf("request beyond the envelope must be rejected before decoding: %v", err)
	}
}
