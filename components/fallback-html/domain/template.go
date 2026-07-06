//go:build !minimal

package domain

import (
	"bytes"
	"errors"
	stdhtml "html"
	"html/template"
	"net/url"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"gorm.io/gorm"
)

const DefaultTemplateID = "generated-portal"

const (
	ContentModeText       = "text"
	ContentModeHTML       = "html"
	ContentModeStaticHTML = "static-html"
)

type TemplateData struct {
	TemplateID string
	SiteName   string
	Title      string
	Body       string
	BodyHTML   template.HTML
	BodyIsHTML bool
	Pages      []Page
}

type TemplateDefinition struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Source             string `json:"source"`
	License            string `json:"license"`
	ContentTypeProfile string `json:"contentTypeProfile"`
}

var generatedPortalTemplate = template.Must(template.New("fallback-generated-portal").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    body{margin:0;min-height:100vh;background:linear-gradient(135deg,#eef2ff,#f8fafc 48%,#ecfeff);color:#111827}
    .shell{max-width:960px;margin:0 auto;padding:48px 24px}
    header{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:72px}
    .brand{font-size:18px;font-weight:750;letter-spacing:.02em}
    nav{display:flex;gap:14px;flex-wrap:wrap}
    nav a{color:#334155;text-decoration:none;font-weight:600}
    main{background:rgba(255,255,255,.74);border:1px solid rgba(148,163,184,.35);border-radius:28px;padding:44px;box-shadow:0 24px 80px rgba(15,23,42,.10)}
    h1{font-size:clamp(34px,6vw,68px);line-height:1;margin:0 0 20px;letter-spacing:-.05em}
    p{font-size:18px;line-height:1.75;color:#475569;max-width:720px}
    footer{margin-top:72px;color:#64748b;font-size:14px}
    @media (prefers-color-scheme:dark){body{background:linear-gradient(135deg,#0f172a,#111827 52%,#083344);color:#f8fafc}.brand,nav a{color:#e2e8f0}main{background:rgba(15,23,42,.72);border-color:rgba(148,163,184,.18)}p,footer{color:#cbd5e1}}
  </style>
</head>
<body>
  <div class="shell">
    <header>
      <div class="brand">{{.SiteName}}</div>
      <nav aria-label="Site navigation">{{range .Pages}}<a href="{{.CanonicalPath}}">{{.Title}}</a>{{end}}</nav>
    </header>
    <main>
      <h1>{{.Title}}</h1>
      {{if .BodyIsHTML}}<div class="content">{{.BodyHTML}}</div>{{else}}<p>{{.Body}}</p>{{end}}
    </main>
    <footer>&copy; {{.SiteName}}</footer>
  </div>
</body>
</html>`))

var knowledgeBaseTemplate = template.Must(template.New("fallback-knowledge-base").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root{color-scheme:light dark;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    body{margin:0;background:#f5f7fb;color:#111827}
    .layout{display:grid;grid-template-columns:minmax(220px,280px) minmax(0,1fr);min-height:100vh}
    aside{padding:32px 24px;background:#fff;border-right:1px solid #e5e7eb}
    .brand{font-size:20px;font-weight:800;margin-bottom:28px}
    nav{display:grid;gap:8px}
    nav a{padding:10px 12px;border-radius:10px;color:#334155;text-decoration:none;font-weight:650}
    nav a:hover{background:#eef2ff}
    main{padding:56px clamp(24px,6vw,88px)}
    article{max-width:820px;background:#fff;border:1px solid #e5e7eb;border-radius:24px;padding:40px;box-shadow:0 18px 50px rgba(15,23,42,.08)}
    h1{font-size:clamp(30px,5vw,56px);line-height:1.05;margin:0 0 20px}
    p{font-size:18px;line-height:1.78;color:#475569}
    footer{margin-top:28px;color:#64748b;font-size:14px}
    @media (max-width:760px){.layout{grid-template-columns:1fr}aside{border-right:0;border-bottom:1px solid #e5e7eb}main{padding:24px}}
    @media (prefers-color-scheme:dark){body{background:#0f172a;color:#f8fafc}aside,article{background:#111827;border-color:#273449}nav a{color:#dbeafe}nav a:hover{background:#1e293b}p,footer{color:#cbd5e1}}
  </style>
</head>
<body>
  <div class="layout">
    <aside>
      <div class="brand">{{.SiteName}}</div>
      <nav aria-label="Site navigation">{{range .Pages}}<a href="{{.CanonicalPath}}">{{.Title}}</a>{{end}}</nav>
    </aside>
    <main>
      <article>
        <h1>{{.Title}}</h1>
        {{if .BodyIsHTML}}<div class="content">{{.BodyHTML}}</div>{{else}}<p>{{.Body}}</p>{{end}}
        <footer>&copy; {{.SiteName}}</footer>
      </article>
    </main>
  </div>
</body>
</html>`))

var webmailWorkspaceTemplate = template.Must(template.New("fallback-webmail-workspace").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    body{margin:0;min-height:100vh;background:#eef3f8;color:#132238}.layout{display:flex;min-height:100vh}.sidebar{width:250px;background:#0f2a43;color:#e8f2ff;padding:28px}.brand{font-size:20px;font-weight:800;margin-bottom:32px}.sidebar a{display:block;color:#cfe2f7;text-decoration:none;padding:11px 13px;border-radius:12px;margin-bottom:6px}.sidebar a:hover{background:#1e4669}.panel{flex:1;padding:42px}.top{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:28px}h1{font-size:42px;margin:0}.badge{background:#dbeafe;color:#1d4ed8;padding:8px 12px;border-radius:999px;font-weight:800}.message{background:#fff;border:1px solid #d9e2ec;border-radius:20px;padding:28px;box-shadow:0 18px 50px rgba(23,43,77,.10)}p{font-size:18px;line-height:1.72;color:#5b6b7e}@media (max-width:760px){.layout{display:block}.sidebar{width:auto;min-height:0}.panel{padding:24px}}@media (prefers-color-scheme:dark){body{background:#071522;color:#e8f2ff}.message{background:#102236;border-color:#213b58}p{color:#9db1c8}.badge{background:#193964;color:#93c5fd}}
  </style>
</head>
<body>
  <div class="layout">
    <aside class="sidebar">
      <div class="brand">{{.SiteName}}</div>
      <nav aria-label="Mailbox navigation">{{range .Pages}}<a href="{{.CanonicalPath}}">{{.Title}}</a>{{end}}</nav>
    </aside>
    <main class="panel">
      <div class="top">
        <h1>{{.Title}}</h1>
        <span class="badge">workspace</span>
      </div>
      <section class="message">
        {{if .BodyIsHTML}}<div class="content">{{.BodyHTML}}</div>{{else}}<p>{{.Body}}</p>{{end}}
      </section>
    </main>
  </div>
</body>
</html>`))

var fileVaultTemplate = template.Must(template.New("fallback-file-vault").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    body{margin:0;min-height:100vh;background:#f6f8fb;color:#172033}.vault{max-width:1120px;margin:0 auto;padding:52px 24px}header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:28px}h1{font-size:48px;line-height:1;margin:0}.nav{display:flex;gap:10px;flex-wrap:wrap}.nav a{color:#2563eb;text-decoration:none;font-weight:800}.card{background:#fff;border:1px solid #dfe7f1;border-radius:24px;padding:30px;box-shadow:0 20px 60px rgba(15,23,42,.08)}p{font-size:18px;line-height:1.72;color:#64748b}.meta{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:14px;margin-top:24px}.meta span{background:#eef2ff;border-radius:16px;padding:16px;font-weight:800;color:#334155}@media (prefers-color-scheme:dark){body{background:#07111d;color:#edf6ff}.card{background:#111f31;border-color:#253b55}p{color:#9fb1c5}.meta span{background:#172b43;color:#dbeafe}.nav a{color:#93c5fd}}
  </style>
</head>
<body>
  <main class="vault">
    <header>
      <div>
        <h1>{{.Title}}</h1>
        <p>{{.SiteName}}</p>
      </div>
      <nav class="nav" aria-label="File navigation">{{range .Pages}}<a href="{{.CanonicalPath}}">{{.Title}}</a>{{end}}</nav>
    </header>
    <section class="card">
      {{if .BodyIsHTML}}<div class="content">{{.BodyHTML}}</div>{{else}}<p>{{.Body}}</p>{{end}}
      <div class="meta"><span>shared files</span><span>sync ready</span><span>archive index</span></div>
    </section>
  </main>
</body>
</html>`))

var statusDashboardTemplate = template.Must(template.New("fallback-status-dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    body{margin:0;background:#f4f7fb;color:#111827;min-height:100vh}.dashboard{max-width:1040px;margin:0 auto;padding:56px 24px}header{display:flex;align-items:center;justify-content:space-between;margin-bottom:30px}h1{font-size:44px;margin:0}.state{background:#dcfce7;color:#166534;padding:9px 14px;border-radius:999px;font-weight:800}.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px}.card,.body{background:#fff;border:1px solid #dfe7f1;border-radius:22px;padding:24px;box-shadow:0 18px 50px rgba(15,23,42,.08)}.card span{display:block;font-size:34px;font-weight:900;margin-top:12px}p{color:#64748b;font-size:18px;line-height:1.7}.body{margin-top:18px}.nav{display:flex;gap:10px;flex-wrap:wrap}.nav a{color:#2563eb;text-decoration:none;font-weight:800}@media (prefers-color-scheme:dark){body{background:#07111d;color:#f8fafc}.card,.body{background:#111f31;border-color:#253b55}p{color:#9fb1c5}.state{background:#123b25;color:#86efac}.nav a{color:#93c5fd}}
  </style>
</head>
<body>
  <main class="dashboard">
    <header>
      <div>
        <h1>{{.Title}}</h1>
        <p>{{.SiteName}}</p>
      </div>
      <span class="state">operational</span>
    </header>
    <section class="cards">
      <article class="card">API Gateway<span>99.98%</span></article>
      <article class="card">Storage<span>99.95%</span></article>
      <article class="card">Sync<span>99.91%</span></article>
    </section>
    <section class="body">
      {{if .BodyIsHTML}}<div class="content">{{.BodyHTML}}</div>{{else}}<p>{{.Body}}</p>{{end}}
      <nav class="nav" aria-label="Status navigation">{{range .Pages}}<a href="{{.CanonicalPath}}">{{.Title}}</a>{{end}}</nav>
    </section>
  </main>
</body>
</html>`))

func BuiltInTemplates() []TemplateDefinition {
	return []TemplateDefinition{
		{
			ID:                 DefaultTemplateID,
			Name:               "Generated portal",
			Source:             "Solovey UI generated template",
			License:            "Project license",
			ContentTypeProfile: "portal",
		},
		{
			ID:                 "knowledge-base",
			Name:               "Knowledge base",
			Source:             "Solovey UI generated template",
			License:            "Project license",
			ContentTypeProfile: "knowledge-base",
		},
		{
			ID:                 "webmail-workspace",
			Name:               "Webmail workspace",
			Source:             "s-ui-fallback-decoys/templates/webmail-workspace; adapted from Tabler MIT page patterns",
			License:            "MIT-adapted",
			ContentTypeProfile: "webmail",
		},
		{
			ID:                 "file-vault",
			Name:               "File vault",
			Source:             "s-ui-fallback-decoys/templates/file-vault; adapted from AdminLTE MIT page patterns",
			License:            "MIT-adapted",
			ContentTypeProfile: "file-cloud",
		},
		{
			ID:                 "status-dashboard",
			Name:               "Status dashboard",
			Source:             "s-ui-fallback-decoys/templates/status-dashboard; adapted from CoreUI MIT page patterns",
			License:            "MIT-adapted",
			ContentTypeProfile: "dashboard",
		},
	}
}

func SeedBuiltInTemplateSources(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	now := time.Now().Unix()
	for _, definition := range BuiltInTemplates() {
		source := TemplateSource{TemplateID: definition.ID}
		if err := db.Where("template_id = ?", definition.ID).Assign(TemplateSource{
			TemplateID:         definition.ID,
			Name:               definition.Name,
			Source:             definition.Source,
			License:            definition.License,
			ContentTypeProfile: definition.ContentTypeProfile,
			Installed:          true,
			CreatedAt:          now,
			UpdatedAt:          now,
		}).FirstOrCreate(&source).Error; err != nil {
			return err
		}
	}
	return nil
}

func RenderGeneratedPage(data TemplateData) ([]byte, error) {
	var buf bytes.Buffer
	if data.BodyIsHTML {
		body, err := SanitizeBodyHTML(data.Body)
		if err != nil {
			return nil, err
		}
		data.BodyHTML = body
	}
	tpl := generatedPortalTemplate
	switch data.TemplateID {
	case "knowledge-base":
		tpl = knowledgeBaseTemplate
	case "webmail-workspace":
		tpl = webmailWorkspaceTemplate
	case "file-vault":
		tpl = fileVaultTemplate
	case "status-dashboard":
		tpl = statusDashboardTemplate
	}
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func NormalizeContentMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", ContentModeText:
		return ContentModeText, nil
	case ContentModeHTML:
		return ContentModeHTML, nil
	case ContentModeStaticHTML:
		return ContentModeStaticHTML, nil
	default:
		return "", ErrUnsupportedContentMode
	}
}

var ErrUnsupportedContentMode = errors.New("content mode must be text, html or static-html")

func SanitizeBodyHTML(value string) (template.HTML, error) {
	nodes, err := xhtml.ParseFragment(strings.NewReader(value), &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, node := range nodes {
		renderSafeHTMLNode(&out, node)
	}
	return template.HTML(out.String()), nil // #nosec G203 -- output is generated by the allowlist sanitizer above.
}

func ValidateStaticHTML(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("static html page is empty")
	}
	root, err := xhtml.Parse(strings.NewReader(value))
	if err != nil {
		return err
	}
	return validateStaticHTMLNode(root)
}

var blockedStaticHTMLTags = map[string]bool{
	"base": true, "embed": true, "form": true, "iframe": true, "input": true, "object": true,
	"script": true, "select": true, "textarea": true,
}

func validateStaticHTMLNode(node *xhtml.Node) error {
	if node.Type == xhtml.ElementNode {
		tag := strings.ToLower(node.Data)
		if blockedStaticHTMLTags[tag] {
			return errors.New("static html templates must not contain <" + tag + ">")
		}
		for _, attr := range node.Attr {
			key := strings.ToLower(strings.TrimSpace(attr.Key))
			value := strings.TrimSpace(attr.Val)
			if strings.HasPrefix(key, "on") {
				return errors.New("static html templates must not contain event handlers")
			}
			if key == "style" && containsExternalCSSURL(value) {
				return errors.New("static html inline styles must not load external URLs")
			}
			switch key {
			case "href", "src":
				if err := validateStaticHTMLURL(tag, key, value); err != nil {
					return err
				}
			case "http-equiv":
				if tag == "meta" && strings.EqualFold(value, "refresh") {
					return errors.New("static html templates must not use meta refresh")
				}
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := validateStaticHTMLNode(child); err != nil {
			return err
		}
	}
	return nil
}

func validateStaticHTMLURL(tag string, attr string, value string) error {
	if value == "" || strings.HasPrefix(value, "#") {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if !parsed.IsAbs() {
		return nil
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if tag == "a" && attr == "href" {
			return nil
		}
		return errors.New("static html templates must not load remote resources")
	default:
		return errors.New("static html templates must not use " + parsed.Scheme + " URLs")
	}
}

func containsExternalCSSURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "url(http://") || strings.Contains(lower, "url(https://") || strings.Contains(lower, "@import")
}

var allowedHTMLTags = map[string]bool{
	"a": true, "blockquote": true, "br": true, "code": true, "em": true, "h2": true, "h3": true,
	"i": true, "li": true, "ol": true, "p": true, "pre": true, "strong": true, "u": true, "ul": true,
}

func renderSafeHTMLNode(out *strings.Builder, node *xhtml.Node) {
	switch node.Type {
	case xhtml.TextNode:
		out.WriteString(stdhtml.EscapeString(node.Data))
	case xhtml.ElementNode:
		tag := strings.ToLower(node.Data)
		if !allowedHTMLTags[tag] {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				renderSafeHTMLNode(out, child)
			}
			return
		}
		out.WriteByte('<')
		out.WriteString(tag)
		if tag == "a" {
			if href := safeHrefAttr(node); href != "" {
				out.WriteString(` href="`)
				out.WriteString(stdhtml.EscapeString(href))
				out.WriteString(`" rel="nofollow noopener noreferrer"`)
			}
		}
		out.WriteByte('>')
		if tag != "br" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				renderSafeHTMLNode(out, child)
			}
			out.WriteString("</")
			out.WriteString(tag)
			out.WriteByte('>')
		}
	case xhtml.DocumentNode:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderSafeHTMLNode(out, child)
		}
	}
}

func safeHrefAttr(node *xhtml.Node) string {
	for _, attr := range node.Attr {
		if strings.ToLower(attr.Key) != "href" {
			continue
		}
		value := strings.TrimSpace(attr.Val)
		if value == "" {
			return ""
		}
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		if parsed.IsAbs() {
			switch strings.ToLower(parsed.Scheme) {
			case "http", "https":
				return parsed.String()
			default:
				return ""
			}
		}
		if strings.HasPrefix(value, "#") {
			return value
		}
		normalized, err := NormalizePublicPath(value)
		if err != nil {
			return ""
		}
		return normalized
	}
	return ""
}
