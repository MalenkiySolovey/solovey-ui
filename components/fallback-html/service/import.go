//go:build !minimal

package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/gorm"
)

type SiteImportInput struct {
	Schema    string               `json:"schema"`
	Pages     []SiteImportPage     `json:"pages"`
	Redirects []SiteImportRedirect `json:"redirects"`
}

type SiteImportPage struct {
	Path          string `json:"path"`
	CanonicalPath string `json:"canonicalPath"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	ContentMode   string `json:"contentMode"`
	IsHome        bool   `json:"isHome"`
	SortOrder     int    `json:"sortOrder"`
}

type SiteImportRedirect struct {
	FromPath   string `json:"fromPath"`
	ToPath     string `json:"toPath"`
	StatusCode int    `json:"statusCode"`
}

type SiteImportResult struct {
	SiteID    uint     `json:"siteId"`
	Pages     int      `json:"pages"`
	Redirects int      `json:"redirects"`
	Warnings  []string `json:"warnings"`
}

func validateImportSchema(schema string) error {
	schema = strings.TrimSpace(schema)
	if schema == "" || schema == "solovey-ui/fallback-html-site/v1" || schema == "solovey-ui/fallback-html-import/v1" {
		return nil
	}
	return fmt.Errorf("unsupported fallback-html import schema %q", schema)
}

func buildImportedPages(siteID uint, inputs []SiteImportPage, reserved []string) ([]fallbackdomain.Page, []string, error) {
	pages := make([]fallbackdomain.Page, 0, len(inputs))
	warnings := []string{}
	seen := map[string]bool{}
	hasHome := false
	for index, input := range inputs {
		path := strings.TrimSpace(input.Path)
		if path == "" {
			path = input.CanonicalPath
		}
		canonical, err := fallbackdomain.ValidatePagePath(path, reserved)
		if err != nil {
			return nil, nil, err
		}
		if seen[canonical] {
			return nil, nil, fmt.Errorf("duplicate imported page path %s", canonical)
		}
		seen[canonical] = true
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
			return nil, nil, err
		}
		if contentMode == fallbackdomain.ContentModeHTML {
			sanitized, err := fallbackdomain.SanitizeBodyHTML(body)
			if err != nil {
				return nil, nil, err
			}
			body = string(sanitized)
		}
		if contentMode == fallbackdomain.ContentModeStaticHTML {
			if err := fallbackdomain.ValidateStaticHTML(body); err != nil {
				return nil, nil, err
			}
		}
		sortOrder := input.SortOrder
		if sortOrder == 0 {
			sortOrder = index + 1
		}
		if input.IsHome && canonical != "/" {
			warnings = append(warnings, "home flag on "+canonical+" was ignored because fallback-html home page must be /")
		}
		isHome := canonical == "/"
		if isHome {
			if hasHome {
				warnings = append(warnings, "multiple imported home flags were collapsed to the first home page")
				isHome = false
			} else {
				hasHome = true
			}
		}
		pages = append(pages, fallbackdomain.Page{
			SiteID:        siteID,
			Path:          canonical,
			CanonicalPath: canonical,
			Title:         title,
			Body:          body,
			ContentMode:   contentMode,
			IsHome:        isHome,
			SortOrder:     sortOrder,
			Provenance:    "imported",
		})
	}
	if !seen["/"] {
		return nil, nil, errors.New("fallback-html import must include a home page at /")
	}
	if !hasHome {
		for index := range pages {
			if pages[index].CanonicalPath == "/" {
				pages[index].IsHome = true
				warnings = append(warnings, "home page flag was added to /")
				break
			}
		}
	}
	return pages, warnings, nil
}

func buildImportedRedirects(siteID uint, inputs []SiteImportRedirect, reserved []string, pages []fallbackdomain.Page) ([]fallbackdomain.Redirect, error) {
	pagePaths := map[string]bool{}
	for _, page := range pages {
		pagePaths[page.CanonicalPath] = true
	}
	seen := map[string]bool{}
	redirects := make([]fallbackdomain.Redirect, 0, len(inputs))
	for _, input := range inputs {
		fromPath, err := fallbackdomain.ValidatePagePath(input.FromPath, reserved)
		if err != nil {
			return nil, err
		}
		if pagePaths[fromPath] {
			return nil, fmt.Errorf("redirect source %s is already an imported page", fromPath)
		}
		if seen[fromPath] {
			return nil, fmt.Errorf("duplicate imported redirect source %s", fromPath)
		}
		toPath, external, err := fallbackdomain.NormalizeRedirectTarget(input.ToPath, reserved)
		if err != nil {
			return nil, err
		}
		if !external && fromPath == toPath {
			return nil, fmt.Errorf("redirect source and target must differ")
		}
		statusCode, err := fallbackdomain.ValidateRedirectStatus(input.StatusCode)
		if err != nil {
			return nil, err
		}
		seen[fromPath] = true
		redirects = append(redirects, fallbackdomain.Redirect{
			SiteID:     siteID,
			FromPath:   fromPath,
			ToPath:     toPath,
			StatusCode: statusCode,
			External:   external,
		})
	}
	return redirects, nil
}

func (s *Service) ImportSite(siteID uint, input SiteImportInput, actor string) (SiteImportResult, error) {
	if err := validateImportSchema(input.Schema); err != nil {
		return SiteImportResult{}, err
	}
	if len(input.Pages) == 0 {
		return SiteImportResult{}, errors.New("fallback-html import requires at least one page")
	}
	if len(input.Pages) > 128 {
		return SiteImportResult{}, errors.New("fallback-html import supports at most 128 pages")
	}
	if len(input.Redirects) > 256 {
		return SiteImportResult{}, errors.New("fallback-html import supports at most 256 redirects")
	}
	reserved, err := s.reservedPaths()
	if err != nil {
		return SiteImportResult{}, err
	}
	pages, warnings, err := buildImportedPages(siteID, input.Pages, reserved)
	if err != nil {
		return SiteImportResult{}, err
	}
	redirects, err := buildImportedRedirects(siteID, input.Redirects, reserved, pages)
	if err != nil {
		return SiteImportResult{}, err
	}
	now := time.Now().Unix()
	err = s.guardedRuntimeMutation(siteID, func(tx *gorm.DB) error {
		if err := tx.First(&fallbackdomain.Site{}, siteID).Error; err != nil {
			return err
		}
		if err := tx.Where("site_id = ?", siteID).Delete(&fallbackdomain.Redirect{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_id = ?", siteID).Delete(&fallbackdomain.Page{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&fallbackdomain.Publish{}).Where("site_id = ?", siteID).Update("active", false).Error; err != nil {
			return err
		}
		for index := range pages {
			pages[index].CreatedAt = now
			pages[index].UpdatedAt = now
		}
		if err := tx.Create(&pages).Error; err != nil {
			return err
		}
		for index := range redirects {
			redirects[index].CreatedAt = now
			redirects[index].UpdatedAt = now
		}
		if len(redirects) > 0 {
			if err := tx.Create(&redirects).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&fallbackdomain.Site{}).Where("id = ?", siteID).Updates(map[string]any{"status": "draft", "updated_at": now}).Error; err != nil {
			return err
		}
		return recordEvent(tx, siteID, actor, "site_imported", map[string]any{"pages": len(pages), "redirects": len(redirects)})
	})
	if err != nil {
		return SiteImportResult{}, err
	}
	return SiteImportResult{SiteID: siteID, Pages: len(pages), Redirects: len(redirects), Warnings: warnings}, nil
}
