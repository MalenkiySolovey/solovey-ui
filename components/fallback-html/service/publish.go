//go:build !minimal

package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/gorm"
)

type PublishResult struct {
	SiteID  uint   `json:"siteId"`
	Version string `json:"version"`
	Files   int    `json:"files"`
}

type PublishView struct {
	ID        uint   `json:"id"`
	Version   string `json:"version"`
	Active    bool   `json:"active"`
	Files     int    `json:"files"`
	CreatedAt int64  `json:"createdAt"`
}

type ArtifactArchive struct {
	Filename    string
	ContentType string
	Data        []byte
	Sha256      string
	SizeBytes   int
}

type RollbackInput struct {
	Version string `json:"version"`
}

type SafetyReport struct {
	OK       bool     `json:"ok"`
	Warnings []string `json:"warnings"`
}

type publishArtifact struct {
	Schema    string                    `json:"schema"`
	Version   string                    `json:"version"`
	CreatedAt int64                     `json:"createdAt"`
	Site      publishArtifactSite       `json:"site"`
	Targets   []TargetView              `json:"targets"`
	Pages     []fallbackdomain.Page     `json:"pages"`
	Redirects []fallbackdomain.Redirect `json:"redirects"`
	Assets    []AssetView               `json:"assets"`
	External  []ExternalResourceView    `json:"externalResources"`
	Files     []publishArtifactFile     `json:"files"`
	Safety    SafetyReport              `json:"safety"`
}

type publishArtifactSite struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	TemplateID   string `json:"templateId"`
	ExposureMode string `json:"exposureMode"`
	Hostname     string `json:"hostname"`
}

type publishArtifactFile struct {
	PublicPath  string `json:"publicPath"`
	Relative    string `json:"relative"`
	MimeType    string `json:"mimeType"`
	Sha256      string `json:"sha256"`
	SizeBytes   int64  `json:"sizeBytes"`
	CachePolicy string `json:"cachePolicy"`
}

type publishArtifactTargets struct {
	Schema    string       `json:"schema"`
	Version   string       `json:"version"`
	CreatedAt int64        `json:"createdAt"`
	Targets   []TargetView `json:"targets"`
}

type publishArtifactRoutes struct {
	Schema       string                         `json:"schema"`
	Version      string                         `json:"version"`
	CreatedAt    int64                          `json:"createdAt"`
	Pages        []publishArtifactRoutePage     `json:"pages"`
	Redirects    []publishArtifactRouteRedirect `json:"redirects"`
	CanonicalMap map[string]string              `json:"canonicalMap"`
}

type publishArtifactRoutePage struct {
	PublicPath string `json:"publicPath"`
	Relative   string `json:"relative"`
	Title      string `json:"title"`
	IsHome     bool   `json:"isHome"`
}

type publishArtifactRouteRedirect struct {
	FromPath   string `json:"fromPath"`
	ToPath     string `json:"toPath"`
	StatusCode int    `json:"statusCode"`
	External   bool   `json:"external"`
}

func (s *Service) ListPublishes(siteID uint) ([]PublishView, error) {
	var publishes []fallbackdomain.Publish
	err := s.db.
		Preload("Files").
		Where("site_id = ?", siteID).
		Order("id DESC").
		Find(&publishes).Error
	if err != nil {
		return nil, err
	}
	out := make([]PublishView, 0, len(publishes))
	for _, publish := range publishes {
		out = append(out, publishView(publish))
	}
	return out, nil
}

func (s *Service) GetPublishArtifact(siteID uint, version string) (ArtifactArchive, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return ArtifactArchive{}, errors.New("publish artifact version is required")
	}
	var publish fallbackdomain.Publish
	err := s.db.
		Preload("Files").
		Preload("Redirects").
		Where("site_id = ? AND version = ?", siteID, version).
		First(&publish).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ArtifactArchive{}, fmt.Errorf("fallback-html publish artifact %s not found", version)
	}
	if err != nil {
		return ArtifactArchive{}, err
	}
	if err := verifyPublishFiles(publish.RootDir, publish.Files); err != nil {
		return ArtifactArchive{}, err
	}
	data, err := buildPublishArchive(publish)
	if err != nil {
		return ArtifactArchive{}, err
	}
	return ArtifactArchive{
		Filename:    "fallback-html-site-" + uintString(siteID) + "-" + safeArchiveName(version) + ".tar.gz",
		ContentType: "application/gzip",
		Data:        data,
		Sha256:      archiveDigest(data),
		SizeBytes:   len(data),
	}, nil
}

func (s *Service) PublishSite(siteID uint, actor string) (PublishResult, error) {
	site, err := s.GetSite(siteID)
	if err != nil {
		return PublishResult{}, err
	}
	if err := s.loadAssets(&site); err != nil {
		return PublishResult{}, err
	}
	externalResources, err := s.externalResourcesForSite(site.ID)
	if err != nil {
		return PublishResult{}, err
	}
	report, err := s.safetyForSite(site)
	if err != nil {
		return PublishResult{}, err
	}
	if !report.OK {
		return PublishResult{}, fmt.Errorf("public site is not safe to publish: %s", strings.Join(report.Warnings, "; "))
	}
	version := strconv.FormatInt(time.Now().UnixNano(), 10)
	root := publishRoot(siteID, version)
	files, err := buildPublishFiles(root, site)
	if err != nil {
		return PublishResult{}, err
	}
	redirects := buildPublishRedirects(site.Redirects)
	now := time.Now().Unix()
	targets, err := s.targetsForSite(site)
	if err != nil {
		return PublishResult{}, err
	}
	if err := s.ensureNoActiveGinPublishConflict(site.ID); err != nil {
		_ = os.RemoveAll(root)
		return PublishResult{}, err
	}
	if err := writePublishArtifact(root, version, now, site, targets, externalResources, files, report); err != nil {
		_ = os.RemoveAll(root)
		return PublishResult{}, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&fallbackdomain.Publish{}).Where("site_id = ?", siteID).Update("active", false).Error; err != nil {
			return err
		}
		publish := fallbackdomain.Publish{SiteID: siteID, Version: version, RootDir: root, Active: true, CreatedAt: now}
		if err := tx.Create(&publish).Error; err != nil {
			return err
		}
		for index := range files {
			files[index].PublishID = publish.ID
		}
		if err := tx.Create(&files).Error; err != nil {
			return err
		}
		for index := range redirects {
			redirects[index].PublishID = publish.ID
		}
		if len(redirects) > 0 {
			if err := tx.Create(&redirects).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&fallbackdomain.Site{}).Where("id = ?", siteID).Updates(map[string]any{"status": "published", "last_error": "", "updated_at": now}).Error; err != nil {
			return err
		}
		if err := createSafetyReport(tx, siteID, report, now); err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "site_published", map[string]any{"version": version, "files": len(files)})
	})
	if err != nil {
		_ = os.RemoveAll(root)
		return PublishResult{}, err
	}
	return PublishResult{SiteID: siteID, Version: version, Files: len(files)}, s.runtime.Rebuild(s.db)
}

func (s *Service) RollbackSite(siteID uint, input RollbackInput, actor string) (PublishResult, error) {
	site, err := s.GetSite(siteID)
	if err != nil {
		return PublishResult{}, err
	}
	if err := s.ensureNoActiveGinPublishConflict(site.ID); err != nil {
		return PublishResult{}, err
	}
	var publish fallbackdomain.Publish
	query := s.db.
		Preload("Files").
		Preload("Redirects").
		Where("site_id = ?", siteID)
	if version := strings.TrimSpace(input.Version); version != "" {
		query = query.Where("version = ?", version)
	} else {
		query = query.Where("active = ?", false)
	}
	if err := query.Order("id DESC").First(&publish).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PublishResult{}, fmt.Errorf("no previous fallback-html publish is available")
		}
		return PublishResult{}, err
	}
	if err := verifyPublishFiles(publish.RootDir, publish.Files); err != nil {
		return PublishResult{}, err
	}
	now := time.Now().Unix()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&fallbackdomain.Publish{}).Where("site_id = ?", siteID).Update("active", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&fallbackdomain.Publish{}).Where("id = ?", publish.ID).Update("active", true).Error; err != nil {
			return err
		}
		if err := tx.Model(&fallbackdomain.Site{}).Where("id = ?", siteID).Updates(map[string]any{"status": "published", "last_error": "", "updated_at": now}).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "site_rollback", map[string]any{"version": publish.Version, "files": len(publish.Files)})
	})
	if err != nil {
		return PublishResult{}, err
	}
	return PublishResult{SiteID: siteID, Version: publish.Version, Files: len(publish.Files)}, s.runtime.Rebuild(s.db)
}

func (s *Service) ensureNoActiveGinPublishConflict(siteID uint) error {
	var conflict fallbackdomain.Site
	err := s.db.
		Model(&fallbackdomain.Site{}).
		Select("fallback_html_sites.*").
		Joins("JOIN fallback_html_publishes ON fallback_html_publishes.site_id = fallback_html_sites.id").
		Where("fallback_html_sites.id <> ? AND fallback_html_sites.enabled = ? AND fallback_html_publishes.active = ?", siteID, true, true).
		Order("fallback_html_publishes.id DESC").
		First(&conflict).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("fallback-html site %q is already published on the managed Gin listener; unpublish it before publishing another site", conflict.Name)
}

func (s *Service) UnpublishSite(siteID uint, actor string) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&fallbackdomain.Publish{}).Where("site_id = ?", siteID).Update("active", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&fallbackdomain.Site{}).Where("id = ?", siteID).Updates(map[string]any{"status": "draft", "updated_at": time.Now().Unix()}).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "site_unpublished", nil)
	})
	if err != nil {
		return err
	}
	return s.runtime.Rebuild(s.db)
}

func buildPublishFiles(root string, site fallbackdomain.Site) ([]fallbackdomain.PublishFile, error) {
	if err := ensureOwnedDir(root, root); err != nil {
		return nil, err
	}
	pages := site.Pages
	if len(pages) == 0 {
		return nil, errors.New("site has no pages")
	}
	files := make([]fallbackdomain.PublishFile, 0, len(pages)+len(site.Assets)+3)
	hasCustomNotFound := false
	for _, page := range pages {
		if page.CanonicalPath == "/404.html" {
			hasCustomNotFound = true
		}
		var rendered []byte
		var err error
		if page.ContentMode == fallbackdomain.ContentModeStaticHTML {
			if err := fallbackdomain.ValidateStaticHTML(page.Body); err != nil {
				return nil, err
			}
			rendered = []byte(page.Body)
		} else {
			rendered, err = fallbackdomain.RenderGeneratedPage(fallbackdomain.TemplateData{
				TemplateID: site.TemplateID,
				SiteName:   site.Name,
				Title:      page.Title,
				Body:       page.Body,
				BodyIsHTML: page.ContentMode == fallbackdomain.ContentModeHTML,
				Pages:      pages,
			})
			if err != nil {
				return nil, err
			}
		}
		file, err := writePublishFile(root, page.CanonicalPath, rendered, "text/html; charset=utf-8", htmlCachePolicy)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	var file fallbackdomain.PublishFile
	var err error
	if !hasCustomNotFound {
		notFound, err := fallbackdomain.RenderGeneratedPage(fallbackdomain.TemplateData{
			TemplateID: site.TemplateID,
			SiteName:   site.Name,
			Title:      "Page not found",
			Body:       "The page you requested is not available.",
			Pages:      pages,
		})
		if err != nil {
			return nil, err
		}
		file, err = writePublishFile(root, "/404.html", notFound, "text/html; charset=utf-8", noStoreCachePolicy)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	for _, asset := range site.Assets {
		data, err := readOwnedRegularFile(assetRoot(site.ID), asset.FilePath)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != asset.Sha256 {
			return nil, fmt.Errorf("fallback-html asset %s failed integrity check", asset.LogicalPath)
		}
		file, err := writePublishFile(root, asset.LogicalPath, data, asset.MimeType, assetCachePolicy)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	robots := []byte("User-agent: *\nDisallow:\n")
	file, err = writePublishFile(root, "/robots.txt", robots, "text/plain; charset=utf-8", serviceCachePolicy)
	if err != nil {
		return nil, err
	}
	files = append(files, file)
	sitemap, err := buildSitemap(pages)
	if err != nil {
		return nil, err
	}
	file, err = writePublishFile(root, "/sitemap.xml", sitemap, "application/xml; charset=utf-8", serviceCachePolicy)
	if err != nil {
		return nil, err
	}
	files = append(files, file)
	return files, nil
}

func buildSitemap(pages []fallbackdomain.Page) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, page := range pages {
		buf.WriteString("  <url><loc>")
		if err := xml.EscapeText(&buf, []byte(page.CanonicalPath)); err != nil {
			return nil, err
		}
		buf.WriteString("</loc></url>\n")
	}
	buf.WriteString("</urlset>\n")
	return buf.Bytes(), nil
}

func buildPublishRedirects(redirects []fallbackdomain.Redirect) []fallbackdomain.PublishRedirect {
	out := make([]fallbackdomain.PublishRedirect, 0, len(redirects))
	for _, redirect := range redirects {
		out = append(out, fallbackdomain.PublishRedirect{
			FromPath:   redirect.FromPath,
			ToPath:     redirect.ToPath,
			StatusCode: redirect.StatusCode,
			External:   redirect.External,
		})
	}
	return out
}

func writePublishArtifact(root string, version string, createdAt int64, site fallbackdomain.Site, targets []TargetView, external []ExternalResourceView, files []fallbackdomain.PublishFile, report SafetyReport) error {
	artifactFiles := make([]publishArtifactFile, 0, len(files))
	relativeByPublicPath := make(map[string]string, len(files))
	var checksums strings.Builder
	for _, file := range files {
		relative, err := filepath.Rel(root, file.FilePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		artifactFiles = append(artifactFiles, publishArtifactFile{
			PublicPath:  file.PublicPath,
			Relative:    relative,
			MimeType:    file.MimeType,
			Sha256:      file.Sha256,
			SizeBytes:   file.SizeBytes,
			CachePolicy: file.CachePolicy,
		})
		relativeByPublicPath[file.PublicPath] = relative
		checksums.WriteString(file.Sha256)
		checksums.WriteByte(' ')
		checksums.WriteString(relative)
		checksums.WriteByte('\n')
	}
	assets := make([]AssetView, 0, len(site.Assets))
	for _, asset := range site.Assets {
		assets = append(assets, assetView(asset))
	}
	artifact := publishArtifact{
		Schema:    "solovey-ui/fallback-html-site/v1",
		Version:   version,
		CreatedAt: createdAt,
		Site: publishArtifactSite{
			ID:           site.ID,
			Name:         site.Name,
			TemplateID:   site.TemplateID,
			ExposureMode: site.ExposureMode,
			Hostname:     site.Hostname,
		},
		Targets:   targets,
		Pages:     site.Pages,
		Redirects: site.Redirects,
		Assets:    assets,
		External:  external,
		Files:     artifactFiles,
		Safety:    report,
	}
	if err := writeJSONFile(root, filepath.Join(root, "site.json"), artifact); err != nil {
		return err
	}
	targetArtifact := publishArtifactTargets{
		Schema:    "solovey-ui/fallback-html-targets/v1",
		Version:   version,
		CreatedAt: createdAt,
		Targets:   targets,
	}
	if err := writeJSONFile(root, filepath.Join(root, "targets.json"), targetArtifact); err != nil {
		return err
	}
	routesArtifact := buildRoutesArtifact(version, createdAt, site, relativeByPublicPath)
	if err := writeJSONFile(root, filepath.Join(root, "routes.json"), routesArtifact); err != nil {
		return err
	}
	if err := writeJSONFile(root, filepath.Join(root, "safety-report.json"), report); err != nil {
		return err
	}
	if err := writeJSONFile(root, filepath.Join(root, "node-artifact.json"), buildNodeArtifactContract(version, createdAt, artifactFiles, report)); err != nil {
		return err
	}
	return writeOwnedNewFile(root, filepath.Join(root, "checksums.txt"), []byte(checksums.String()), 0o640)
}

func buildRoutesArtifact(version string, createdAt int64, site fallbackdomain.Site, relativeByPublicPath map[string]string) publishArtifactRoutes {
	pages := make([]publishArtifactRoutePage, 0, len(site.Pages))
	canonicalMap := make(map[string]string, len(site.Pages)+len(site.Redirects))
	for _, page := range site.Pages {
		pages = append(pages, publishArtifactRoutePage{
			PublicPath: page.CanonicalPath,
			Relative:   relativeByPublicPath[page.CanonicalPath],
			Title:      page.Title,
			IsHome:     page.IsHome,
		})
		canonicalMap[page.Path] = page.CanonicalPath
	}
	redirects := make([]publishArtifactRouteRedirect, 0, len(site.Redirects))
	for _, redirect := range site.Redirects {
		redirects = append(redirects, publishArtifactRouteRedirect{
			FromPath:   redirect.FromPath,
			ToPath:     redirect.ToPath,
			StatusCode: redirect.StatusCode,
			External:   redirect.External,
		})
		canonicalMap[redirect.FromPath] = redirect.ToPath
	}
	return publishArtifactRoutes{
		Schema:       "solovey-ui/fallback-html-routes/v1",
		Version:      version,
		CreatedAt:    createdAt,
		Pages:        pages,
		Redirects:    redirects,
		CanonicalMap: canonicalMap,
	}
}

func buildPublishArchive(publish fallbackdomain.Publish) ([]byte, error) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	modTime := time.Unix(publish.CreatedAt, 0).UTC()

	metadata := []string{"site.json", "targets.json", "routes.json", "safety-report.json", "node-artifact.json", "checksums.txt"}
	for _, relative := range metadata {
		if err := addArchiveFile(tarWriter, publish.RootDir, filepath.Join(publish.RootDir, relative), relative, modTime); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return nil, err
		}
	}
	for _, file := range publish.Files {
		relative, err := filepath.Rel(publish.RootDir, file.FilePath)
		if err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return nil, err
		}
		relative = filepath.ToSlash(relative)
		if err := addArchiveFile(tarWriter, publish.RootDir, file.FilePath, relative, modTime); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func addArchiveFile(writer *tar.Writer, root string, filePath string, archiveName string, modTime time.Time) error {
	archiveName = filepath.ToSlash(strings.TrimSpace(archiveName))
	if err := validateArchiveName(archiveName); err != nil {
		return err
	}
	data, err := readOwnedRegularFile(root, filePath)
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name:    archiveName,
		Mode:    0o640,
		Size:    int64(len(data)),
		ModTime: modTime,
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func validateArchiveName(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return fmt.Errorf("invalid fallback-html artifact path %q", value)
	}
	if value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") || strings.HasSuffix(value, "/..") {
		return fmt.Errorf("invalid fallback-html artifact path %q", value)
	}
	return nil
}

func safeArchiveName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "artifact"
	}
	return b.String()
}

func writeJSONFile(root, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeOwnedNewFile(root, path, data, 0o640)
}

func verifyPublishFiles(root string, files []fallbackdomain.PublishFile) error {
	if len(files) == 0 {
		return errors.New("publish has no files")
	}
	for _, file := range files {
		data, err := readOwnedRegularFile(root, file.FilePath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.Sha256 {
			return fmt.Errorf("fallback-html publish file %s failed integrity check", file.PublicPath)
		}
	}
	return nil
}

func createSafetyReport(tx *gorm.DB, siteID uint, report SafetyReport, now int64) error {
	warnings, err := json.Marshal(report.Warnings)
	if err != nil {
		return err
	}
	return tx.Create(&fallbackdomain.SafetyReport{
		SiteID:    siteID,
		OK:        report.OK,
		Warnings:  warnings,
		CreatedAt: now,
	}).Error
}

func writePublishFile(root, publicPath string, data []byte, mimeType string, cache string) (fallbackdomain.PublishFile, error) {
	relative := fallbackdomain.PublicPathToFilePath(publicPath)
	diskPath := filepath.Join(root, relative)
	if err := writeOwnedNewFile(root, diskPath, data, 0o640); err != nil {
		return fallbackdomain.PublishFile{}, err
	}
	sum := sha256.Sum256(data)
	return fallbackdomain.PublishFile{
		PublicPath:  publicPath,
		FilePath:    diskPath,
		MimeType:    mimeType,
		Sha256:      hex.EncodeToString(sum[:]),
		SizeBytes:   int64(len(data)),
		CachePolicy: cache,
	}, nil
}
