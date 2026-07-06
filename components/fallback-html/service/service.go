//go:build !minimal

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

type Service struct {
	db               *gorm.DB
	runtime          *Runtime
	nodeClient       NodeClient
	templateHTTP     *http.Client
	remoteCatalogURL string
}

type SiteInput struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Enabled    *bool  `json:"enabled"`
	TemplateID string `json:"templateId"`
}

type PageInput struct {
	ID          uint   `json:"id"`
	Path        string `json:"path"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	ContentMode string `json:"contentMode"`
	IsHome      bool   `json:"isHome"`
}

type PathValidationInput struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type PathValidationResult struct {
	OK               bool   `json:"ok"`
	CanonicalPath    string `json:"canonicalPath"`
	ReservedConflict bool   `json:"reservedConflict"`
	Message          string `json:"message"`
}

type RedirectInput struct {
	ID         uint   `json:"id"`
	FromPath   string `json:"fromPath"`
	ToPath     string `json:"toPath"`
	StatusCode int    `json:"statusCode"`
}

type ExternalResourceInput struct {
	ID      uint   `json:"id"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Allowed bool   `json:"allowed"`
}

type PreviewInput struct {
	PageID uint   `json:"pageId"`
	Path   string `json:"path"`
}

type PreviewResult struct {
	Path     string   `json:"path"`
	HTML     string   `json:"html"`
	Warnings []string `json:"warnings"`
}

func New(db *gorm.DB, runtime *Runtime) *Service {
	if runtime == nil {
		runtime = DefaultRuntime
	}
	return &Service{db: db, runtime: runtime, nodeClient: NewHTTPNodeClient(nil)}
}

func HandleRestorePostOpen(ctx context.Context, db *gorm.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		DefaultRuntime.Stop()
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	migrator := db.Migrator()
	if !migrator.HasTable(&fallbackdomain.Site{}) {
		return DefaultRuntime.Rebuild(db)
	}
	hasPublishes := migrator.HasTable(&fallbackdomain.Publish{})
	hasEvents := migrator.HasTable(&fallbackdomain.Event{})
	now := time.Now().Unix()
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hasPublishes {
			if err := tx.Model(&fallbackdomain.Publish{}).Where("active = ?", true).Update("active", false).Error; err != nil {
				return err
			}
		}
		message := "restored from database backup; publish again to recreate verified public files"
		if err := tx.Model(&fallbackdomain.Site{}).Where("1 = 1").Updates(map[string]any{
			"status":     "draft",
			"last_error": message,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if !hasEvents {
			return nil
		}
		var sites []fallbackdomain.Site
		if err := tx.Select("id").Find(&sites).Error; err != nil {
			return err
		}
		for _, site := range sites {
			if err := recordEvent(tx, site.ID, "system", "site_restore_deactivated", map[string]any{
				"reason": message,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return DefaultRuntime.Rebuild(db)
}

func (s *Service) ListSites() ([]fallbackdomain.Site, error) {
	var sites []fallbackdomain.Site
	err := s.db.
		Preload("Pages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC, id ASC") }).
		Preload("Redirects", func(db *gorm.DB) *gorm.DB { return db.Order("from_path ASC, id ASC") }).
		Preload("Targets").
		Order("id ASC").
		Find(&sites).Error
	return sites, err
}

func (s *Service) CreateSiteFromTemplate(templateID string, actor string) (fallbackdomain.Site, error) {
	if site, ok, err := s.createSiteFromInstalledTemplate(templateID, actor); ok || err != nil {
		return site, err
	}
	definition, err := templateDefinitionByID(templateID)
	if err != nil {
		return fallbackdomain.Site{}, err
	}
	return s.SaveSite(SiteInput{Name: definition.Name, TemplateID: definition.ID}, actor)
}

func (s *Service) RuntimeStatus() RuntimeStatus {
	return s.runtime.Status()
}

func (s *Service) SaveSite(input SiteInput, actor string) (fallbackdomain.Site, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Public site"
	}
	templateID, err := s.normalizeTemplateID(input.TemplateID)
	if err != nil {
		return fallbackdomain.Site{}, err
	}
	now := time.Now().Unix()
	var site fallbackdomain.Site
	defaultTarget, err := s.currentWebTarget()
	if err != nil {
		return site, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if input.ID != 0 {
			if err := tx.First(&site, input.ID).Error; err != nil {
				return err
			}
		} else {
			site = fallbackdomain.Site{
				Enabled:      true,
				ExposureMode: "direct",
				Status:       "draft",
				CreatedAt:    now,
			}
		}
		site.Name = name
		site.TemplateID = templateID
		if input.Enabled != nil {
			site.Enabled = *input.Enabled
		}
		site.UpdatedAt = now
		if err := tx.Save(&site).Error; err != nil {
			return err
		}
		if input.ID == 0 {
			if err := createDefaultPages(tx, site.ID, site.Name, now); err != nil {
				return err
			}
			target := targetFromView(site.ID, defaultTarget, now)
			if err := tx.Create(&target).Error; err != nil {
				return err
			}
		}
		return recordEvent(tx, site.ID, actor, "site_saved", map[string]any{"name": site.Name})
	})
	if err != nil {
		return site, err
	}
	_ = s.runtime.Rebuild(s.db)
	return s.GetSite(site.ID)
}

func (s *Service) normalizeTemplateID(value string) (string, error) {
	templateID := strings.TrimSpace(value)
	if templateID == "" {
		templateID = fallbackdomain.DefaultTemplateID
	}
	if _, err := templateDefinitionByID(templateID); err == nil {
		return templateID, nil
	}
	if _, ok, err := s.installedTemplatePackage(templateID); err != nil {
		return "", err
	} else if ok {
		return templateID, nil
	}
	var count int64
	if err := s.db.Model(&fallbackdomain.Site{}).Where("template_id = ?", templateID).Count(&count).Error; err != nil {
		return "", err
	}
	if count > 0 {
		return templateID, nil
	}
	return "", fmt.Errorf("unknown fallback-html template %q", templateID)
}

func templateDefinitionByID(templateID string) (fallbackdomain.TemplateDefinition, error) {
	return builtInTemplateDefinition(templateID)
}

func (s *Service) GetSite(id uint) (fallbackdomain.Site, error) {
	var site fallbackdomain.Site
	err := s.db.
		Preload("Pages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC, id ASC") }).
		Preload("Redirects", func(db *gorm.DB) *gorm.DB { return db.Order("from_path ASC, id ASC") }).
		Preload("Assets", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		Preload("Targets").
		First(&site, id).Error
	return site, err
}

func (s *Service) ListPages(siteID uint) ([]fallbackdomain.Page, error) {
	var pages []fallbackdomain.Page
	err := s.db.
		Order("sort_order ASC, id ASC").
		Where("site_id = ?", siteID).
		Find(&pages).Error
	return pages, err
}

func (s *Service) DeleteSite(id uint, actor string) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := recordEvent(tx, id, actor, "site_deleted", nil); err != nil {
			return err
		}
		return tx.Delete(&fallbackdomain.Site{}, id).Error
	})
	if err != nil {
		return err
	}
	removeErr := RemoveSiteStorage(id)
	rebuildErr := s.runtime.Rebuild(s.db)
	return errors.Join(removeErr, rebuildErr)
}

func (s *Service) ValidatePath(siteID uint, input PathValidationInput) (PathValidationResult, error) {
	reserved, err := s.reservedPaths()
	if err != nil {
		return PathValidationResult{}, err
	}
	canonical, err := fallbackdomain.ValidatePagePath(input.Path, reserved)
	result := PathValidationResult{CanonicalPath: canonical}
	if err != nil {
		normalized, normalizeErr := fallbackdomain.NormalizePublicPath(input.Path)
		if normalizeErr == nil {
			result.CanonicalPath = normalized
			result.ReservedConflict = fallbackdomain.IsReservedPublicPath(normalized, reserved)
		}
		result.Message = err.Error()
		return result, nil
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "page"
	}
	var conflicts int64
	switch kind {
	case "page":
		err = s.db.Model(&fallbackdomain.Redirect{}).Where("site_id = ? AND from_path = ?", siteID, canonical).Count(&conflicts).Error
	case "redirect":
		err = s.db.Model(&fallbackdomain.Page{}).Where("site_id = ? AND canonical_path = ?", siteID, canonical).Count(&conflicts).Error
	default:
		result.Message = "path kind must be page or redirect"
		return result, nil
	}
	if err != nil {
		return PathValidationResult{}, err
	}
	if conflicts > 0 {
		result.Message = "path is already used by another public route"
		return result, nil
	}
	result.OK = true
	return result, nil
}

func (s *Service) SavePage(siteID uint, input PageInput, actor string) (fallbackdomain.Page, error) {
	reserved, err := s.reservedPaths()
	if err != nil {
		return fallbackdomain.Page{}, err
	}
	canonical, err := fallbackdomain.ValidatePagePath(input.Path, reserved)
	if err != nil {
		return fallbackdomain.Page{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "Page"
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		body = "This page is available."
	}
	contentMode, err := fallbackdomain.NormalizeContentMode(input.ContentMode)
	if err != nil {
		return fallbackdomain.Page{}, err
	}
	if contentMode == fallbackdomain.ContentModeHTML {
		sanitized, err := fallbackdomain.SanitizeBodyHTML(body)
		if err != nil {
			return fallbackdomain.Page{}, err
		}
		body = string(sanitized)
	}
	if contentMode == fallbackdomain.ContentModeStaticHTML {
		if err := fallbackdomain.ValidateStaticHTML(body); err != nil {
			return fallbackdomain.Page{}, err
		}
	}
	now := time.Now().Unix()
	var page fallbackdomain.Page
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&fallbackdomain.Site{}, siteID).Error; err != nil {
			return err
		}
		var redirectCount int64
		if err := tx.Model(&fallbackdomain.Redirect{}).Where("site_id = ? AND from_path = ?", siteID, canonical).Count(&redirectCount).Error; err != nil {
			return err
		}
		if redirectCount > 0 {
			return fmt.Errorf("page path %s is already a redirect", canonical)
		}
		if input.ID != 0 {
			if err := tx.Where("site_id = ?", siteID).First(&page, input.ID).Error; err != nil {
				return err
			}
		} else {
			page = fallbackdomain.Page{SiteID: siteID, Provenance: "generated", CreatedAt: now}
		}
		page.Path = canonical
		page.CanonicalPath = canonical
		page.Title = title
		page.Body = body
		page.ContentMode = contentMode
		page.IsHome = input.IsHome || canonical == "/"
		page.UpdatedAt = now
		if err := tx.Save(&page).Error; err != nil {
			return err
		}
		if page.IsHome {
			if err := tx.Model(&fallbackdomain.Page{}).Where("site_id = ? AND id <> ?", siteID, page.ID).Update("is_home", false).Error; err != nil {
				return err
			}
		}
		return recordEvent(tx, siteID, actor, "page_saved", map[string]any{"path": canonical})
	})
	return page, err
}

func (s *Service) DeletePage(siteID, pageID uint, actor string) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&fallbackdomain.Page{}).Where("site_id = ?", siteID).Count(&count).Error; err != nil {
			return err
		}
		if count <= 1 {
			return fmt.Errorf("site must keep at least one page")
		}
		if err := tx.Where("site_id = ?", siteID).Delete(&fallbackdomain.Page{}, pageID).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "page_deleted", map[string]any{"pageId": pageID})
	})
	return err
}

func (s *Service) ListRedirects(siteID uint) ([]fallbackdomain.Redirect, error) {
	var redirects []fallbackdomain.Redirect
	err := s.db.
		Order("from_path ASC, id ASC").
		Where("site_id = ?", siteID).
		Find(&redirects).Error
	return redirects, err
}

func (s *Service) SaveRedirect(siteID uint, input RedirectInput, actor string) (fallbackdomain.Redirect, error) {
	reserved, err := s.reservedPaths()
	if err != nil {
		return fallbackdomain.Redirect{}, err
	}
	fromPath, err := fallbackdomain.ValidatePagePath(input.FromPath, reserved)
	if err != nil {
		return fallbackdomain.Redirect{}, err
	}
	toPath, external, err := fallbackdomain.NormalizeRedirectTarget(input.ToPath, reserved)
	if err != nil {
		return fallbackdomain.Redirect{}, err
	}
	if !external && fromPath == toPath {
		return fallbackdomain.Redirect{}, fmt.Errorf("redirect source and target must differ")
	}
	statusCode, err := fallbackdomain.ValidateRedirectStatus(input.StatusCode)
	if err != nil {
		return fallbackdomain.Redirect{}, err
	}
	now := time.Now().Unix()
	var redirect fallbackdomain.Redirect
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&fallbackdomain.Site{}, siteID).Error; err != nil {
			return err
		}
		var pageCount int64
		if err := tx.Model(&fallbackdomain.Page{}).Where("site_id = ? AND canonical_path = ?", siteID, fromPath).Count(&pageCount).Error; err != nil {
			return err
		}
		if pageCount > 0 {
			return fmt.Errorf("redirect source %s is already a page", fromPath)
		}
		if input.ID != 0 {
			if err := tx.Where("site_id = ?", siteID).First(&redirect, input.ID).Error; err != nil {
				return err
			}
		} else {
			redirect = fallbackdomain.Redirect{SiteID: siteID, CreatedAt: now}
		}
		var duplicateCount int64
		if err := tx.Model(&fallbackdomain.Redirect{}).Where("site_id = ? AND from_path = ? AND id <> ?", siteID, fromPath, redirect.ID).Count(&duplicateCount).Error; err != nil {
			return err
		}
		if duplicateCount > 0 {
			return fmt.Errorf("redirect source %s already exists", fromPath)
		}
		redirect.FromPath = fromPath
		redirect.ToPath = toPath
		redirect.External = external
		redirect.StatusCode = statusCode
		redirect.UpdatedAt = now
		if err := tx.Save(&redirect).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "redirect_saved", map[string]any{"from": fromPath, "external": external})
	})
	return redirect, err
}

func (s *Service) DeleteRedirect(siteID, redirectID uint, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("site_id = ?", siteID).Delete(&fallbackdomain.Redirect{}, redirectID).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "redirect_deleted", map[string]any{"redirectId": redirectID})
	})
}

func (s *Service) Safety(siteID uint) (SafetyReport, error) {
	site, err := s.GetSite(siteID)
	if err != nil {
		return SafetyReport{}, err
	}
	return s.safetyForSite(site)
}

func (s *Service) PreviewSite(siteID uint, input PreviewInput, actor string) (PreviewResult, error) {
	site, err := s.GetSite(siteID)
	if err != nil {
		return PreviewResult{}, err
	}
	if len(site.Pages) == 0 {
		return PreviewResult{}, errors.New("site has no pages")
	}
	page := site.Pages[0]
	if input.PageID != 0 {
		found := false
		for _, candidate := range site.Pages {
			if candidate.ID == input.PageID {
				page = candidate
				found = true
				break
			}
		}
		if !found {
			return PreviewResult{}, fmt.Errorf("preview page not found")
		}
	} else if strings.TrimSpace(input.Path) != "" {
		canonical, err := fallbackdomain.NormalizePublicPath(input.Path)
		if err != nil {
			return PreviewResult{}, err
		}
		found := false
		for _, candidate := range site.Pages {
			if candidate.CanonicalPath == canonical {
				page = candidate
				found = true
				break
			}
		}
		if !found {
			return PreviewResult{}, fmt.Errorf("preview page %s not found", canonical)
		}
	} else {
		for _, candidate := range site.Pages {
			if candidate.IsHome || candidate.CanonicalPath == "/" {
				page = candidate
				break
			}
		}
	}
	var rendered []byte
	if page.ContentMode == fallbackdomain.ContentModeStaticHTML {
		if err := fallbackdomain.ValidateStaticHTML(page.Body); err != nil {
			return PreviewResult{}, err
		}
		rendered = []byte(page.Body)
	} else {
		rendered, err = fallbackdomain.RenderGeneratedPage(fallbackdomain.TemplateData{
			TemplateID: site.TemplateID,
			SiteName:   site.Name,
			Title:      page.Title,
			Body:       page.Body,
			BodyIsHTML: page.ContentMode == fallbackdomain.ContentModeHTML,
			Pages:      site.Pages,
		})
		if err != nil {
			return PreviewResult{}, err
		}
	}
	report, err := s.safetyForSite(site)
	if err != nil {
		return PreviewResult{}, err
	}
	if err := recordEvent(s.db, siteID, actor, "site_previewed", map[string]any{"path": page.CanonicalPath}); err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Path: page.CanonicalPath, HTML: string(rendered), Warnings: report.Warnings}, nil
}

func (s *Service) reservedPaths() ([]string, error) {
	settings := coreservice.SettingService{}
	webPath, err := settings.GetWebPath()
	if err != nil {
		return nil, err
	}
	subPath, err := settings.GetSubPath()
	if err != nil {
		return nil, err
	}
	return []string{webPath, subPath}, nil
}

func (s *Service) safetyForSite(site fallbackdomain.Site) (SafetyReport, error) {
	var warnings []string
	settings := coreservice.SettingService{}
	webPath, err := settings.GetWebPath()
	if err != nil {
		return SafetyReport{}, err
	}
	if webPath == "/" {
		warnings = append(warnings, "admin web path is '/', move the panel to a secret path before publishing")
	}
	if webPath == "/app/" {
		warnings = append(warnings, "admin web path is still the default /app/")
	}
	reserved, err := s.reservedPaths()
	if err != nil {
		return SafetyReport{}, err
	}
	hasHome := false
	for _, page := range site.Pages {
		if page.IsHome || page.CanonicalPath == "/" {
			hasHome = true
		}
		if fallbackdomain.IsReservedPublicPath(page.CanonicalPath, reserved) {
			warnings = append(warnings, "page "+page.CanonicalPath+" conflicts with reserved panel paths")
		}
	}
	for _, redirect := range site.Redirects {
		if fallbackdomain.IsReservedPublicPath(redirect.FromPath, reserved) {
			warnings = append(warnings, "redirect "+redirect.FromPath+" conflicts with reserved panel paths")
		}
		if !redirect.External && fallbackdomain.IsReservedPublicPath(redirect.ToPath, reserved) {
			warnings = append(warnings, "redirect "+redirect.FromPath+" targets a reserved panel path")
		}
	}
	if !hasHome {
		warnings = append(warnings, "site does not have a home page at /")
	}
	targets, err := s.targetsForSite(site)
	if err != nil {
		return SafetyReport{}, err
	}
	for _, target := range targets {
		if target.Status != "" && target.Status != "available" {
			warnings = append(warnings, "publish target "+target.Kind+" is "+target.Status+": "+target.Reason)
		}
		if target.Kind == "web-current" {
			if fallback, reason := addressWouldFallback(target.Listen); fallback {
				warnings = append(warnings, "publish target would use loopback fallback: "+reason)
			}
		}
	}
	return SafetyReport{OK: len(warnings) == 0, Warnings: warnings}, nil
}

func createDefaultPages(tx *gorm.DB, siteID uint, siteName string, now int64) error {
	pages := []fallbackdomain.Page{
		{SiteID: siteID, Path: "/", CanonicalPath: "/", Title: siteName, Body: "A simple public site is available here.", ContentMode: "text", IsHome: true, SortOrder: 1, Provenance: "generated", CreatedAt: now, UpdatedAt: now},
		{SiteID: siteID, Path: "/about/", CanonicalPath: "/about/", Title: "About", Body: "This page keeps a normal public surface on this host.", ContentMode: "text", SortOrder: 2, Provenance: "generated", CreatedAt: now, UpdatedAt: now},
	}
	return tx.Create(&pages).Error
}

func recordEvent(tx *gorm.DB, siteID uint, actor string, action string, details map[string]any) error {
	raw := json.RawMessage(nil)
	if details != nil {
		data, err := json.Marshal(details)
		if err != nil {
			return err
		}
		raw = data
	}
	return tx.Create(&fallbackdomain.Event{
		SiteID:    siteID,
		Actor:     actor,
		Action:    action,
		Details:   raw,
		CreatedAt: time.Now().Unix(),
	}).Error
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
