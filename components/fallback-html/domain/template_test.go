//go:build !minimal

package domain

import (
	"strings"
	"testing"
)

func TestBuiltInTemplatesRenderSafePublicHTML(t *testing.T) {
	pages := []Page{
		{CanonicalPath: "/", Title: "Home", IsHome: true},
		{CanonicalPath: "/about/", Title: "About"},
	}
	markers := map[string]string{
		DefaultTemplateID:   "Site navigation",
		"knowledge-base":    "Site navigation",
		"webmail-workspace": "Mailbox navigation",
		"file-vault":        "File navigation",
		"status-dashboard":  "Status navigation",
	}
	for _, definition := range BuiltInTemplates() {
		t.Run(definition.ID, func(t *testing.T) {
			html, err := RenderGeneratedPage(TemplateData{
				TemplateID: definition.ID,
				SiteName:   "Example",
				Title:      "Home",
				Body:       `<strong>Hello</strong><script>alert(1)</script>`,
				BodyIsHTML: true,
				Pages:      pages,
			})
			if err != nil {
				t.Fatalf("RenderGeneratedPage: %v", err)
			}
			text := string(html)
			if marker := markers[definition.ID]; marker != "" && !strings.Contains(text, marker) {
				t.Fatalf("template marker %q missing: %s", marker, text)
			}
			for _, forbidden := range []string{"<script", "javascript:", "/api", "/sub/", "/app/", "/secret-panel/"} {
				if strings.Contains(strings.ToLower(text), forbidden) {
					t.Fatalf("rendered template leaks forbidden token %q: %s", forbidden, text)
				}
			}
			if definition.ContentTypeProfile == "" || definition.Source == "" || definition.License == "" {
				t.Fatalf("template definition misses provenance/profile: %#v", definition)
			}
		})
	}
}
