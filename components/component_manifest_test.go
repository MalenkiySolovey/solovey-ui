package components

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestComponentJSONManifestsAreValid(t *testing.T) {
	for _, id := range []string{
		"import-xui",
		"observability-extra",
		"paid-subscriptions",
		"panel-update-ui",
		"remote-outbound-subscriptions",
		"telegram",
	} {
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
