//go:build !minimal

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
	"gorm.io/gorm"
)

const (
	defaultRemoteTemplateCatalogURL = "https://raw.githubusercontent.com/MalenkiySolovey/solovey-fallback-pages/main/templates/catalog.json"
	remoteTemplateHTTPTimeout       = 15 * time.Second
	maxRemoteTemplateFileBytes      = 1024 * 1024
)

type RemoteTemplateCatalog struct {
	CatalogURL string               `json:"catalogUrl"`
	Templates  []RemoteTemplateView `json:"templates"`
}

type RemoteTemplateView struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	License            string   `json:"license"`
	Source             string   `json:"source"`
	ContentTypeProfile string   `json:"contentTypeProfile"`
	ManifestURL        string   `json:"manifestUrl"`
	Installed          bool     `json:"installed"`
	InstalledAt        int64    `json:"installedAt"`
	Notes              []string `json:"notes"`
}

type remoteCatalogFile struct {
	Schema    string                     `json:"schema"`
	Templates []remoteCatalogTemplateRef `json:"templates"`
}

type remoteCatalogTemplateRef struct {
	ID       string `json:"id"`
	Manifest string `json:"manifest"`
}

type remoteTemplateManifest struct {
	Schema             string           `json:"schema"`
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	License            string           `json:"license"`
	Source             remoteSourceInfo `json:"source"`
	ContentTypeProfile string           `json:"contentTypeProfile"`
	Pages              []string         `json:"pages"`
	Assets             []string         `json:"assets"`
	Notes              []string         `json:"notes"`
}

type remoteSourceInfo struct {
	Repository     string   `json:"repository"`
	License        string   `json:"license"`
	ReferenceFiles []string `json:"referenceFiles"`
}

type installedTemplatePackage struct {
	Manifest remoteTemplateManifest
	Raw      []byte
}

func (s *Service) ListRemoteTemplateCatalog(ctx context.Context) (RemoteTemplateCatalog, error) {
	catalogURL := s.templateCatalogURL()
	catalog, err := s.fetchRemoteCatalog(ctx, catalogURL)
	if err != nil {
		return RemoteTemplateCatalog{}, err
	}
	installed, err := s.installedTemplateSources()
	if err != nil {
		return RemoteTemplateCatalog{}, err
	}
	out := RemoteTemplateCatalog{CatalogURL: catalogURL, Templates: make([]RemoteTemplateView, 0, len(catalog.Templates))}
	for _, ref := range catalog.Templates {
		manifestURL, err := resolveRemoteTemplateURL(catalogURL, ref.Manifest)
		if err != nil {
			return RemoteTemplateCatalog{}, err
		}
		manifest, raw, err := s.fetchRemoteManifest(ctx, manifestURL)
		if err != nil {
			return RemoteTemplateCatalog{}, err
		}
		if err := validateRemoteManifest(ref.ID, manifest); err != nil {
			return RemoteTemplateCatalog{}, err
		}
		source := installed[manifest.ID]
		out.Templates = append(out.Templates, RemoteTemplateView{
			ID:                 manifest.ID,
			Name:               manifest.Name,
			License:            manifest.License,
			Source:             remoteSourceLabel(manifest.Source),
			ContentTypeProfile: manifest.ContentTypeProfile,
			ManifestURL:        manifestURL,
			Installed:          len(source.ManifestJSON) > 0,
			InstalledAt:        source.UpdatedAt,
			Notes:              manifest.Notes,
		})
		_ = raw
	}
	return out, nil
}

func (s *Service) InstallRemoteTemplate(ctx context.Context, templateID string, actor string) (RemoteTemplateView, error) {
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return RemoteTemplateView{}, errors.New("remote template id is required")
	}
	catalogURL := s.templateCatalogURL()
	catalog, err := s.fetchRemoteCatalog(ctx, catalogURL)
	if err != nil {
		return RemoteTemplateView{}, err
	}
	var manifestURL string
	found := false
	for _, ref := range catalog.Templates {
		if ref.ID != templateID {
			continue
		}
		manifestURL, err = resolveRemoteTemplateURL(catalogURL, ref.Manifest)
		if err != nil {
			return RemoteTemplateView{}, err
		}
		found = true
		break
	}
	if !found {
		return RemoteTemplateView{}, fmt.Errorf("remote fallback-html template %q was not found in catalog", templateID)
	}
	manifest, raw, err := s.fetchRemoteManifest(ctx, manifestURL)
	if err != nil {
		return RemoteTemplateView{}, err
	}
	if err := validateRemoteManifest(templateID, manifest); err != nil {
		return RemoteTemplateView{}, err
	}
	if err := s.downloadRemoteTemplateFiles(ctx, manifestURL, manifest); err != nil {
		return RemoteTemplateView{}, err
	}
	now := time.Now().Unix()
	source := fallbackdomain.TemplateSource{TemplateID: manifest.ID}
	err = s.db.Where("template_id = ?", manifest.ID).Assign(fallbackdomain.TemplateSource{
		TemplateID:         manifest.ID,
		Name:               manifest.Name,
		Source:             remoteSourceLabel(manifest.Source),
		License:            manifest.License,
		ContentTypeProfile: manifest.ContentTypeProfile,
		CatalogURL:         catalogURL,
		ManifestURL:        manifestURL,
		ManifestJSON:       raw,
		Installed:          true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}).FirstOrCreate(&source).Error
	if err != nil {
		return RemoteTemplateView{}, err
	}
	if err := recordEvent(s.db, 0, actor, "remote_template_installed", map[string]any{"templateId": manifest.ID}); err != nil {
		return RemoteTemplateView{}, err
	}
	return RemoteTemplateView{
		ID:                 manifest.ID,
		Name:               manifest.Name,
		License:            manifest.License,
		Source:             remoteSourceLabel(manifest.Source),
		ContentTypeProfile: manifest.ContentTypeProfile,
		ManifestURL:        manifestURL,
		Installed:          true,
		InstalledAt:        now,
		Notes:              manifest.Notes,
	}, nil
}

func (s *Service) DeleteRemoteTemplate(templateID string, actor string) error {
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return errors.New("remote template id is required")
	}
	var source fallbackdomain.TemplateSource
	err := s.db.Where("template_id = ? AND manifest_json <> ''", templateID).First(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("installed remote fallback-html template %q was not found", templateID)
	}
	if err != nil {
		return err
	}
	if err := os.RemoveAll(templateRoot(templateID)); err != nil {
		return err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if isBuiltInTemplateID(templateID) {
			definition, err := builtInTemplateDefinition(templateID)
			if err != nil {
				return err
			}
			now := time.Now().Unix()
			if err := tx.Model(&source).Updates(map[string]any{
				"name":                 definition.Name,
				"source":               definition.Source,
				"license":              definition.License,
				"content_type_profile": definition.ContentTypeProfile,
				"catalog_url":          "",
				"manifest_url":         "",
				"manifest_json":        nil,
				"installed":            true,
				"updated_at":           now,
			}).Error; err != nil {
				return err
			}
		} else if err := tx.Delete(&source).Error; err != nil {
			return err
		}
		return recordEvent(tx, 0, actor, "remote_template_deleted", map[string]any{"templateId": templateID})
	})
	return err
}

func (s *Service) createSiteFromInstalledTemplate(templateID string, actor string) (fallbackdomain.Site, bool, error) {
	pkg, ok, err := s.installedTemplatePackage(templateID)
	if err != nil || !ok {
		return fallbackdomain.Site{}, ok, err
	}
	site, err := s.createSiteFromRemotePackage(pkg, actor)
	return site, true, err
}

func (s *Service) installedTemplatePackage(templateID string) (installedTemplatePackage, bool, error) {
	var source fallbackdomain.TemplateSource
	err := s.db.Where("template_id = ? AND manifest_json <> ''", templateID).First(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return installedTemplatePackage{}, false, nil
	}
	if err != nil {
		return installedTemplatePackage{}, false, err
	}
	var manifest remoteTemplateManifest
	if err := json.Unmarshal(source.ManifestJSON, &manifest); err != nil {
		return installedTemplatePackage{}, false, err
	}
	if err := validateRemoteManifest(templateID, manifest); err != nil {
		return installedTemplatePackage{}, false, err
	}
	return installedTemplatePackage{Manifest: manifest, Raw: source.ManifestJSON}, true, nil
}

func (s *Service) createSiteFromRemotePackage(pkg installedTemplatePackage, actor string) (fallbackdomain.Site, error) {
	if len(pkg.Manifest.Pages) == 0 {
		return fallbackdomain.Site{}, errors.New("remote fallback-html template has no pages")
	}
	now := time.Now().Unix()
	defaultTarget, err := s.currentWebTarget()
	if err != nil {
		return fallbackdomain.Site{}, err
	}
	var site fallbackdomain.Site
	err = s.db.Transaction(func(tx *gorm.DB) error {
		site = fallbackdomain.Site{
			Name:         pkg.Manifest.Name,
			Enabled:      true,
			TemplateID:   pkg.Manifest.ID,
			ExposureMode: "direct",
			Status:       "draft",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := tx.Create(&site).Error; err != nil {
			return err
		}
		target := targetFromView(site.ID, defaultTarget, now)
		if err := tx.Create(&target).Error; err != nil {
			return err
		}
		assetMap, err := s.installTemplateAssets(tx, site.ID, pkg.Manifest)
		if err != nil {
			return err
		}
		pages, err := buildTemplatePages(site.ID, pkg.Manifest, assetMap)
		if err != nil {
			return err
		}
		if err := tx.Create(&pages).Error; err != nil {
			return err
		}
		return recordEvent(tx, site.ID, actor, "site_created_from_remote_template", map[string]any{"templateId": pkg.Manifest.ID, "pages": len(pages)})
	})
	if err != nil {
		if site.ID != 0 {
			_ = RemoveSiteStorage(site.ID)
		}
		return fallbackdomain.Site{}, err
	}
	_ = s.runtime.Rebuild(s.db)
	return s.GetSite(site.ID)
}

func (s *Service) installTemplateAssets(tx *gorm.DB, siteID uint, manifest remoteTemplateManifest) (map[string]string, error) {
	payloads := make([]templateAssetPayload, 0, len(manifest.Assets))
	for _, assetPath := range manifest.Assets {
		clean, err := cleanRemoteTemplateFilePath(assetPath)
		if err != nil {
			return nil, err
		}
		data := decoyInteractivityScript
		if !isDecoyInteractivityAsset(clean) {
			data, err = readOwnedRegularFile(templateRoot(manifest.ID), filepath.Join(templateRoot(manifest.ID), filepath.FromSlash(clean)))
			if err != nil {
				return nil, err
			}
		}
		payloads = append(payloads, templateAssetPayload{Path: clean, Data: data})
	}
	sort.Slice(payloads, func(left, right int) bool { return payloads[left].Path < payloads[right].Path })
	result := make(map[string]string, len(payloads))
	for _, payload := range payloads {
		if path.Ext(payload.Path) == ".css" {
			continue
		}
		logical, err := templateAssetLogicalPath(payload.Path, payload.Data)
		if err != nil {
			return nil, err
		}
		result[payload.Path] = logical
	}
	for index := range payloads {
		if path.Ext(payloads[index].Path) != ".css" {
			continue
		}
		payloads[index].Data = rewriteTemplateCSSAssetReferences(payloads[index].Path, payloads[index].Data, result)
		logical, err := templateAssetLogicalPath(payloads[index].Path, payloads[index].Data)
		if err != nil {
			return nil, err
		}
		result[payloads[index].Path] = logical
	}
	for _, payload := range payloads {
		var view AssetView
		var err error
		if isDecoyInteractivityAsset(payload.Path) {
			view, err = s.saveDecoyInteractivityAssetInTx(tx, siteID, "template:"+manifest.ID)
		} else {
			view, err = s.saveAssetInTx(tx, siteID, path.Base(payload.Path), payload.Data, "template:"+manifest.ID)
		}
		if err != nil {
			return nil, err
		}
		if view.LogicalPath != result[payload.Path] {
			return nil, fmt.Errorf("fallback template asset %s changed while installing", payload.Path)
		}
	}
	return result, nil
}

type templateAssetPayload struct {
	Path string
	Data []byte
}

func isDecoyInteractivityAsset(value string) bool {
	return strings.EqualFold(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")), decoyInteractivityAssetPath)
}

func templateAssetLogicalPath(assetPath string, data []byte) (string, error) {
	if isDecoyInteractivityAsset(assetPath) {
		sum := sha256.Sum256(decoyInteractivityScript)
		return fallbackdomain.ValidatePagePath("/media/"+hex.EncodeToString(sum[:])[:12]+"-decoy-interactivity.js", nil)
	}
	return assetLogicalPath(path.Base(assetPath), data)
}

func (s *Service) saveDecoyInteractivityAssetInTx(tx *gorm.DB, siteID uint, provenance string) (AssetView, error) {
	if err := tx.First(&fallbackdomain.Site{}, siteID).Error; err != nil {
		return AssetView{}, err
	}
	if err := ensureSiteAssetQuota(tx, siteID, int64(len(decoyInteractivityScript))); err != nil {
		return AssetView{}, err
	}
	sum := sha256.Sum256(decoyInteractivityScript)
	sha := hex.EncodeToString(sum[:])
	logicalPath, err := fallbackdomain.ValidatePagePath("/media/"+sha[:12]+"-decoy-interactivity.js", nil)
	if err != nil {
		return AssetView{}, err
	}
	diskPath := filepath.Join(assetRoot(siteID), sha[:12]+"-decoy-interactivity.js")
	if err := writeOwnedNewFile(assetRoot(siteID), diskPath, decoyInteractivityScript, 0o640); err != nil {
		return AssetView{}, err
	}
	asset := fallbackdomain.Asset{
		SiteID: siteID, LogicalPath: logicalPath, FilePath: diskPath,
		MimeType: "application/javascript; charset=utf-8", Sha256: sha,
		SizeBytes: int64(len(decoyInteractivityScript)), Provenance: provenance,
		CreatedAt: time.Now().Unix(),
	}
	if err := tx.Create(&asset).Error; err != nil {
		return AssetView{}, err
	}
	return assetView(asset), nil
}

func (s *Service) saveAssetInTx(tx *gorm.DB, siteID uint, filename string, data []byte, provenance string) (AssetView, error) {
	if err := tx.First(&fallbackdomain.Site{}, siteID).Error; err != nil {
		return AssetView{}, err
	}
	if len(data) == 0 {
		return AssetView{}, errors.New("asset file is empty")
	}
	if len(data) > maxAssetBytes {
		return AssetView{}, fmt.Errorf("asset file is larger than %d bytes", maxAssetBytes)
	}
	safeName, mimeType, err := validateAssetFile(filename, data)
	if err != nil {
		return AssetView{}, err
	}
	if err := ensureSiteAssetQuota(tx, siteID, int64(len(data))); err != nil {
		return AssetView{}, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	logicalPath, err := fallbackdomain.ValidatePagePath("/media/"+sha[:12]+"-"+safeName, nil)
	if err != nil {
		return AssetView{}, err
	}
	diskPath := filepath.Join(assetRoot(siteID), sha[:12]+"-"+safeName)
	if err := writeOwnedNewFile(assetRoot(siteID), diskPath, data, 0o640); err != nil {
		return AssetView{}, err
	}
	asset := fallbackdomain.Asset{
		SiteID:      siteID,
		LogicalPath: logicalPath,
		FilePath:    diskPath,
		MimeType:    mimeType,
		Sha256:      sha,
		SizeBytes:   int64(len(data)),
		Provenance:  provenance,
		CreatedAt:   time.Now().Unix(),
	}
	if err := tx.Create(&asset).Error; err != nil {
		return AssetView{}, err
	}
	return assetView(asset), nil
}

func buildTemplatePages(siteID uint, manifest remoteTemplateManifest, assetMap map[string]string) ([]fallbackdomain.Page, error) {
	pages := make([]fallbackdomain.Page, 0, len(manifest.Pages))
	seen := map[string]bool{}
	now := time.Now().Unix()
	for index, pageFile := range manifest.Pages {
		clean, err := cleanRemoteTemplateFilePath(pageFile)
		if err != nil {
			return nil, err
		}
		publicPath, err := templatePagePublicPath(clean)
		if err != nil {
			return nil, err
		}
		if seen[publicPath] {
			return nil, fmt.Errorf("duplicate remote template page path %s", publicPath)
		}
		seen[publicPath] = true
		data, err := readOwnedRegularFile(templateRoot(manifest.ID), filepath.Join(templateRoot(manifest.ID), filepath.FromSlash(clean)))
		if err != nil {
			return nil, err
		}
		html := rewriteTemplateAssetReferences(clean, string(data), assetMap)
		if err := fallbackdomain.ValidateDecoyTemplateHTML(html); err != nil {
			return nil, err
		}
		pages = append(pages, fallbackdomain.Page{
			SiteID:        siteID,
			Path:          publicPath,
			CanonicalPath: publicPath,
			Title:         templatePageTitle(publicPath, manifest.Name),
			Body:          html,
			ContentMode:   fallbackdomain.ContentModeStaticHTML,
			IsHome:        publicPath == "/",
			SortOrder:     index + 1,
			Provenance:    "template:" + manifest.ID,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	if !seen["/"] {
		return nil, errors.New("remote fallback-html template must include pages/index.html")
	}
	return pages, nil
}

func (s *Service) ListTemplates() []fallbackdomain.TemplateDefinition {
	definitions := fallbackdomain.BuiltInTemplates()
	var sources []fallbackdomain.TemplateSource
	if err := s.db.Where("manifest_json <> ''").Order("template_id ASC").Find(&sources).Error; err != nil {
		return definitions
	}
	known := make(map[string]int, len(definitions))
	for index, definition := range definitions {
		known[definition.ID] = index
	}
	for _, source := range sources {
		definition := fallbackdomain.TemplateDefinition{
			ID:                 source.TemplateID,
			Name:               source.Name,
			Source:             source.Source,
			License:            source.License,
			ContentTypeProfile: source.ContentTypeProfile,
			Renderable:         isBuiltInTemplateID(source.TemplateID),
		}
		if index, ok := known[source.TemplateID]; ok {
			definitions[index] = definition
		} else {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

func (s *Service) templateCatalogURL() string {
	if value := strings.TrimSpace(s.remoteCatalogURL); value != "" {
		return value
	}
	return defaultRemoteTemplateCatalogURL
}

func (s *Service) templateHTTPClient() *http.Client {
	if s.templateHTTP != nil {
		return s.templateHTTP
	}
	return ssrf.NewHTTPClient(remoteTemplateHTTPTimeout, "https")
}

func (s *Service) fetchRemoteCatalog(ctx context.Context, catalogURL string) (remoteCatalogFile, error) {
	var catalog remoteCatalogFile
	if err := s.fetchJSON(ctx, catalogURL, &catalog); err != nil {
		return catalog, err
	}
	if catalog.Schema != "solovey-ui/fallback-decoy-catalog/v1" {
		return catalog, fmt.Errorf("unsupported fallback template catalog schema %q", catalog.Schema)
	}
	if len(catalog.Templates) == 0 {
		return catalog, errors.New("fallback template catalog is empty")
	}
	return catalog, nil
}

func (s *Service) fetchRemoteManifest(ctx context.Context, manifestURL string) (remoteTemplateManifest, []byte, error) {
	data, err := s.fetchBytes(ctx, manifestURL)
	if err != nil {
		return remoteTemplateManifest{}, nil, err
	}
	var manifest remoteTemplateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return remoteTemplateManifest{}, nil, err
	}
	return manifest, data, nil
}

func (s *Service) fetchJSON(ctx context.Context, rawURL string, out any) error {
	data, err := s.fetchBytes(ctx, rawURL)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (s *Service) fetchBytes(ctx context.Context, rawURL string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.templateHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("fallback template catalog returned %s", response.Status)
	}
	return readLimitedResponse(response, maxRemoteTemplateFileBytes)
}

func readLimitedResponse(response *http.Response, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("remote fallback template file is larger than %d bytes", limit)
	}
	return data, nil
}

func (s *Service) downloadRemoteTemplateFiles(ctx context.Context, manifestURL string, manifest remoteTemplateManifest) error {
	root := templateRoot(manifest.ID)
	tempRoot := root + ".tmp"
	_ = os.RemoveAll(tempRoot)
	if err := ensureOwnedDir(tempRoot, tempRoot); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempRoot)
		}
	}()
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestData = append(manifestData, '\n')
	if err := writeOwnedNewFile(tempRoot, filepath.Join(tempRoot, "manifest.json"), manifestData, 0o640); err != nil {
		return err
	}
	for _, item := range append(append([]string{}, manifest.Pages...), manifest.Assets...) {
		clean, err := cleanRemoteTemplateFilePath(item)
		if err != nil {
			return err
		}
		if isDecoyInteractivityAsset(clean) {
			continue
		}
		fileURL, err := resolveRemoteTemplateURL(manifestURL, clean)
		if err != nil {
			return err
		}
		data, err := s.fetchBytes(ctx, fileURL)
		if err != nil {
			return err
		}
		if path.Ext(clean) == ".html" {
			html := rewriteTemplateAssetReferences(clean, string(data), nil)
			if err := fallbackdomain.ValidateDecoyTemplateHTML(html); err != nil {
				return err
			}
		} else if _, _, err := validateAssetFile(path.Base(clean), data); err != nil {
			return err
		}
		if err := writeOwnedNewFile(tempRoot, filepath.Join(tempRoot, filepath.FromSlash(clean)), data, 0o640); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(root)
	if err := os.Rename(tempRoot, root); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func resolveRemoteTemplateURL(baseURL string, ref string) (string, error) {
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsedRef, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", err
	}
	resolved := parsedBase.ResolveReference(parsedRef)
	if resolved.Scheme != "https" && resolved.Scheme != "http" {
		return "", fmt.Errorf("unsupported remote template URL scheme %q", resolved.Scheme)
	}
	if resolved.Scheme != parsedBase.Scheme || !strings.EqualFold(resolved.Host, parsedBase.Host) {
		return "", errors.New("remote fallback template files must stay on the catalog host")
	}
	return resolved.String(), nil
}

func validateRemoteManifest(expectedID string, manifest remoteTemplateManifest) error {
	if manifest.Schema != "solovey-ui/fallback-decoy-template/v1" {
		return fmt.Errorf("unsupported fallback template schema %q", manifest.Schema)
	}
	if strings.TrimSpace(manifest.ID) == "" || manifest.ID != expectedID {
		return fmt.Errorf("fallback template id mismatch: %q != %q", manifest.ID, expectedID)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return errors.New("fallback template name is required")
	}
	if len(manifest.Pages) == 0 {
		return errors.New("fallback template must include pages")
	}
	seen := make(map[string]struct{}, len(manifest.Pages)+len(manifest.Assets))
	for _, item := range manifest.Pages {
		clean, err := cleanRemoteTemplateFilePath(item)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(clean, "pages/") || path.Ext(clean) != ".html" {
			return fmt.Errorf("fallback template page %q must be an html file under pages/", item)
		}
		if _, exists := seen[clean]; exists {
			return fmt.Errorf("fallback template lists %q more than once", item)
		}
		seen[clean] = struct{}{}
	}
	hasDecoyScript := false
	for _, item := range manifest.Assets {
		clean, err := cleanRemoteTemplateFilePath(item)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(clean, "assets/") {
			return fmt.Errorf("fallback template asset %q must be under assets/", item)
		}
		if _, exists := seen[clean]; exists {
			return fmt.Errorf("fallback template lists %q more than once", item)
		}
		seen[clean] = struct{}{}
		hasDecoyScript = hasDecoyScript || isDecoyInteractivityAsset(clean)
	}
	if !hasDecoyScript {
		return errors.New("fallback template must declare assets/decoy-interactivity.js")
	}
	return nil
}

func cleanRemoteTemplateFilePath(value string) (string, error) {
	clean := path.Clean(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || clean == ".." || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("invalid fallback template file path %q", value)
	}
	ext := strings.ToLower(path.Ext(clean))
	if ext != ".html" && ext != ".css" && ext != ".ico" && ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".txt" && ext != ".webp" && ext != ".woff" && ext != ".woff2" && ext != ".js" {
		return "", fmt.Errorf("fallback template file extension %s is not allowed", ext)
	}
	if ext == ".js" && !isDecoyInteractivityAsset(clean) {
		return "", errors.New("fallback template may only declare the controlled decoy-interactivity.js script")
	}
	return clean, nil
}

func templatePagePublicPath(pageFile string) (string, error) {
	clean, err := cleanRemoteTemplateFilePath(pageFile)
	if err != nil {
		return "", err
	}
	if path.Ext(clean) != ".html" {
		return "", fmt.Errorf("remote template page %s must be html", clean)
	}
	relative := strings.TrimPrefix(clean, "pages/")
	dir := path.Dir(relative)
	name := strings.TrimSuffix(path.Base(relative), ".html")
	if name == "index" {
		if dir == "." {
			return "/", nil
		}
		return "/" + strings.Trim(dir, "/") + "/", nil
	}
	if name == "404" || name == "500" {
		if dir == "." {
			return "/" + name + ".html", nil
		}
		return "/" + strings.Trim(path.Join(dir, name+".html"), "/"), nil
	}
	if dir == "." {
		return "/" + name + "/", nil
	}
	return "/" + strings.Trim(path.Join(dir, name), "/") + "/", nil
}

func templatePageTitle(publicPath string, fallbackName string) string {
	trimmed := strings.Trim(publicPath, "/")
	if trimmed == "" {
		return fallbackName
	}
	trimmed = strings.TrimSuffix(trimmed, ".html")
	parts := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(trimmed, "-", " "), "_", " "))
	for i := range parts {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, " ")
}

func rewriteTemplateAssetReferences(pageFile string, html string, assetMap map[string]string) string {
	if len(assetMap) == 0 {
		return html
	}
	for assetRel, logical := range assetMap {
		refs := []string{assetRel, "/" + assetRel}
		if rel, err := filepath.Rel(filepath.FromSlash(path.Dir(pageFile)), filepath.FromSlash(assetRel)); err == nil {
			rel = filepath.ToSlash(rel)
			refs = append(refs, rel)
		}
		for _, ref := range refs {
			html = strings.ReplaceAll(html, `href="`+ref+`"`, `href="`+logical+`"`)
			html = strings.ReplaceAll(html, `src="`+ref+`"`, `src="`+logical+`"`)
			html = strings.ReplaceAll(html, `href='`+ref+`'`, `href='`+logical+`'`)
			html = strings.ReplaceAll(html, `src='`+ref+`'`, `src='`+logical+`'`)
		}
	}
	return html
}

var templateCSSURLPattern = regexp.MustCompile(`(?i)url\(\s*(?:['"])?([^'"\)]+)(?:['"])?\s*\)`)

func rewriteTemplateCSSAssetReferences(assetFile string, data []byte, assetMap map[string]string) []byte {
	if len(assetMap) == 0 {
		return data
	}
	return templateCSSURLPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := templateCSSURLPattern.FindSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		reference := strings.TrimSpace(string(parts[1]))
		if reference == "" || strings.HasPrefix(reference, "#") || strings.HasPrefix(strings.ToLower(reference), "data:") || strings.Contains(reference, "://") || strings.HasPrefix(reference, "//") {
			return match
		}
		resolved := path.Clean(path.Join(path.Dir(assetFile), strings.TrimPrefix(reference, "/")))
		logical, ok := assetMap[resolved]
		if !ok {
			return match
		}
		return []byte(`url("` + logical + `")`)
	})
}

func (s *Service) installedTemplateSources() (map[string]fallbackdomain.TemplateSource, error) {
	var sources []fallbackdomain.TemplateSource
	if err := s.db.Where("manifest_json <> ''").Find(&sources).Error; err != nil {
		return nil, err
	}
	out := make(map[string]fallbackdomain.TemplateSource, len(sources))
	for _, source := range sources {
		out[source.TemplateID] = source
	}
	return out, nil
}

func remoteSourceLabel(source remoteSourceInfo) string {
	if source.Repository == "" {
		return "solovey-fallback-pages"
	}
	if source.License == "" {
		return source.Repository
	}
	return source.Repository + " (" + source.License + ")"
}

func isBuiltInTemplateID(templateID string) bool {
	_, err := builtInTemplateDefinition(templateID)
	return err == nil
}

func builtInTemplateDefinition(templateID string) (fallbackdomain.TemplateDefinition, error) {
	for _, definition := range fallbackdomain.BuiltInTemplates() {
		if definition.ID == templateID {
			return definition, nil
		}
	}
	return fallbackdomain.TemplateDefinition{}, fmt.Errorf("unknown fallback-html template %q", templateID)
}
