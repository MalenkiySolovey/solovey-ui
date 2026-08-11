package components

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestComponentJSONManifestsAreValid(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(entry.Name(), "component.json")); err == nil {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatal("no component manifests discovered")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(id, "component.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := manifest.FromJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := parsed.Validate(); err != nil {
				t.Fatal(err)
			}
			if parsed.ID != id {
				t.Fatalf("component manifest id = %q, want %q", parsed.ID, id)
			}
			if parsed.Since == "" {
				t.Fatalf("component manifest %q must declare since", id)
			}
		})
	}
}
