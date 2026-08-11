//go:build !minimal

package domain

import (
	"fmt"
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

func TestValidateDecoyTemplateHTMLAllowsPassiveControls(t *testing.T) {
	html := `<!doctype html><html><head><link rel="stylesheet" href="../assets/site.css"></head><body><form><input type="email"><button type="submit">Sign in</button></form><script src="../assets/decoy-interactivity.js"></script></body></html>`
	if err := ValidateDecoyTemplateHTML(html); err != nil {
		t.Fatalf("ValidateDecoyTemplateHTML: %v", err)
	}
}

func TestValidateDecoyTemplateHTMLRejectsUnsafeContent(t *testing.T) {
	base := `<!doctype html><html><body>%s<script src="../assets/decoy-interactivity.js"></script></body></html>`
	for _, content := range []string{
		`<form action="https://example.com"><input></form>`,
		`<input name="password">`,
		`<script src="https://example.com/app.js"></script>`,
		`<style>@import url(https://example.com/site.css);</style>`,
		`<!--[if lte IE 8]><script src="legacy.js"></script><![endif]-->`,
	} {
		if err := ValidateDecoyTemplateHTML(fmt.Sprintf(base, content)); err == nil {
			t.Fatalf("unsafe static template was accepted: %s", content)
		}
	}
}
