//go:build !minimal

package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/gorm"
)

// backendStore retains concrete transaction and durable reservation authority.
type backendStore struct {
	db      *gorm.DB
	runtime *Runtime
}

func backendStoreFor(db *gorm.DB, runtimes ...*Runtime) Store {
	if db == nil {
		return nil
	}
	runtime := DefaultRuntime
	if len(runtimes) > 0 && runtimes[0] != nil {
		runtime = runtimes[0]
	}
	return &backendStore{db: db, runtime: runtime}
}
func (b *backendStore) Context() context.Context { return b.db.Statement.Context }
func (b *backendStore) WithContext(ctx context.Context) Store {
	return &backendStore{db: b.db.WithContext(ctx), runtime: b.runtime}
}
func (b *backendStore) Transaction(call func(Store) error) error {
	return b.db.Transaction(func(tx *gorm.DB) error { return call(&backendStore{db: tx, runtime: b.runtime}) })
}
func (b *backendStore) WithMutationLock(call func() error) error {
	return authority.WithMutationLock(call)
}
func (b *backendStore) WithMutationLockContext(ctx context.Context, call func() error) error {
	return authority.WithMutationLockContext(ctx, call)
}
func (b *backendStore) GuardSiteMutation(siteID uint) error {
	return authority.GuardSiteMutation(b.db, authority.ProviderID, "site:"+strconv.FormatUint(uint64(siteID), 10), time.Now().UTC())
}

// Preserve the concrete Backend error for API and compatibility callers while
// exposing the portable missing-record classification to feature workflows.
type storeError struct{ cause error }

func (e storeError) Error() string { return e.cause.Error() }
func (e storeError) Unwrap() error { return e.cause }
func (e storeError) Is(target error) bool {
	return target == ErrRecordNotFound || errors.Is(e.cause, target)
}
func normalizeStoreError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storeError{err}
	}
	return err
}
func (b *backendStore) ListSites(out *[]fallbackdomain.Site) error {
	return normalizeStoreError(b.db.Preload("Pages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC, id ASC") }).Preload("Redirects", func(db *gorm.DB) *gorm.DB { return db.Order("from_path ASC, id ASC") }).Preload("Targets").Order("id ASC").Find(out).Error)
}
func (b *backendStore) LoadSite(id uint, out *fallbackdomain.Site) error {
	return normalizeStoreError(b.db.First(out, id).Error)
}
func (b *backendStore) LoadSiteDetails(id uint, out *fallbackdomain.Site) error {
	return normalizeStoreError(b.db.Preload("Pages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC, id ASC") }).Preload("Redirects", func(db *gorm.DB) *gorm.DB { return db.Order("from_path ASC, id ASC") }).Preload("Assets", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).Preload("Targets").First(out, id).Error)
}
func (b *backendStore) LoadSiteTargets(id uint, out *fallbackdomain.Site) error {
	return normalizeStoreError(b.db.Preload("Targets", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).First(out, id).Error)
}
func (b *backendStore) SaveSite(site *fallbackdomain.Site) error {
	return normalizeStoreError(b.db.Save(site).Error)
}
func (b *backendStore) CreateSite(site *fallbackdomain.Site) error {
	return normalizeStoreError(b.db.Create(site).Error)
}
func (b *backendStore) SetSiteDisabled(id uint) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Site{}).Where("id = ?", id).Update("enabled", false).Error)
}
func (b *backendStore) DeleteSite(id uint) error {
	return normalizeStoreError(b.db.Delete(&fallbackdomain.Site{}, id).Error)
}
func (b *backendStore) CountTemplateSites(templateID string, out *int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Site{}).Where("template_id = ?", templateID).Count(out).Error)
}
func (b *backendStore) ListPages(siteID uint, out *[]fallbackdomain.Page) error {
	return normalizeStoreError(b.db.Order("sort_order ASC, id ASC").Where("site_id = ?", siteID).Find(out).Error)
}
func (b *backendStore) LoadPage(siteID, id uint, out *fallbackdomain.Page) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).First(out, id).Error)
}
func (b *backendStore) SavePage(page *fallbackdomain.Page) error {
	return normalizeStoreError(b.db.Save(page).Error)
}
func (b *backendStore) CreatePages(pages *[]fallbackdomain.Page) error {
	return normalizeStoreError(b.db.Create(pages).Error)
}
func (b *backendStore) ClearOtherHomePages(siteID, exceptID uint) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Page{}).Where("site_id = ? AND id <> ?", siteID, exceptID).Update("is_home", false).Error)
}
func (b *backendStore) CountPages(siteID uint, out *int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Page{}).Where("site_id = ?", siteID).Count(out).Error)
}
func (b *backendStore) CountPagePath(siteID uint, canonical string, out *int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Page{}).Where("site_id = ? AND canonical_path = ?", siteID, canonical).Count(out).Error)
}
func (b *backendStore) DeletePage(siteID, id uint) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).Delete(&fallbackdomain.Page{}, id).Error)
}
func (b *backendStore) DeletePages(siteID uint) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).Delete(&fallbackdomain.Page{}).Error)
}
func (b *backendStore) ListRedirects(siteID uint, out *[]fallbackdomain.Redirect) error {
	return normalizeStoreError(b.db.Order("from_path ASC, id ASC").Where("site_id = ?", siteID).Find(out).Error)
}
func (b *backendStore) LoadRedirect(siteID, id uint, out *fallbackdomain.Redirect) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).First(out, id).Error)
}
func (b *backendStore) SaveRedirect(redirect *fallbackdomain.Redirect) error {
	return normalizeStoreError(b.db.Save(redirect).Error)
}
func (b *backendStore) CreateRedirects(redirects *[]fallbackdomain.Redirect) error {
	return normalizeStoreError(b.db.Create(redirects).Error)
}
func (b *backendStore) CountRedirectPath(siteID uint, canonical string, out *int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Redirect{}).Where("site_id = ? AND from_path = ?", siteID, canonical).Count(out).Error)
}
func (b *backendStore) CountOtherRedirectPath(siteID uint, canonical string, exceptID uint, out *int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Redirect{}).Where("site_id = ? AND from_path = ? AND id <> ?", siteID, canonical, exceptID).Count(out).Error)
}
func (b *backendStore) DeleteRedirect(siteID, id uint) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).Delete(&fallbackdomain.Redirect{}, id).Error)
}
func (b *backendStore) DeleteRedirects(siteID uint) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).Delete(&fallbackdomain.Redirect{}).Error)
}
func (b *backendStore) ListAssets(siteID uint, out *[]fallbackdomain.Asset) error {
	return normalizeStoreError(b.db.Order("logical_path ASC, id ASC").Where("site_id = ?", siteID).Find(out).Error)
}
func (b *backendStore) ListStyleAssets(siteID uint, out *[]fallbackdomain.Asset) error {
	return normalizeStoreError(b.db.Where("site_id = ? AND mime_type LIKE ?", siteID, "text/css%").Find(out).Error)
}
func (b *backendStore) LoadAssetPath(siteID uint, logicalPath string, out *fallbackdomain.Asset) error {
	return normalizeStoreError(b.db.Where("site_id = ? AND logical_path = ?", siteID, logicalPath).First(out).Error)
}
func (b *backendStore) LoadAsset(siteID, id uint, out *fallbackdomain.Asset) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).First(out, id).Error)
}
func (b *backendStore) CreateAsset(asset *fallbackdomain.Asset) error {
	return normalizeStoreError(b.db.Create(asset).Error)
}
func (b *backendStore) DeleteAsset(asset *fallbackdomain.Asset) error {
	return normalizeStoreError(b.db.Delete(asset).Error)
}
func (b *backendStore) AssetBytesUsed(siteID uint, out *int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Asset{}).Where("site_id = ?", siteID).Select("COALESCE(SUM(size_bytes), 0)").Scan(out).Error)
}
func (b *backendStore) ListExternalResources(siteID uint, out *[]fallbackdomain.ExternalResource) error {
	return normalizeStoreError(b.db.Order("kind ASC, url ASC, id ASC").Where("site_id = ?", siteID).Find(out).Error)
}
func (b *backendStore) LoadExternalResource(siteID, id uint, out *fallbackdomain.ExternalResource) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).First(out, id).Error)
}
func (b *backendStore) SaveExternalResource(resource *fallbackdomain.ExternalResource) error {
	return normalizeStoreError(b.db.Save(resource).Error)
}
func (b *backendStore) DeleteExternalResource(siteID, id uint) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).Delete(&fallbackdomain.ExternalResource{}, id).Error)
}
func (b *backendStore) ListPublishes(siteID uint, out *[]fallbackdomain.Publish) error {
	return normalizeStoreError(b.db.Preload("Files").Where("site_id = ?", siteID).Order("id DESC").Find(out).Error)
}
func (b *backendStore) LoadPublish(siteID uint, version string, out *fallbackdomain.Publish) error {
	return normalizeStoreError(b.db.Preload("Files").Preload("Redirects").Where("site_id = ? AND version = ?", siteID, version).First(out).Error)
}
func (b *backendStore) DeactivatePublishes(siteID uint) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Publish{}).Where("site_id = ?", siteID).Update("active", false).Error)
}
func (b *backendStore) ActivatePublish(id uint) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Publish{}).Where("id = ?", id).Update("active", true).Error)
}
func (b *backendStore) CreatePublish(publish *fallbackdomain.Publish) error {
	return normalizeStoreError(b.db.Create(publish).Error)
}
func (b *backendStore) CreatePublishFiles(files *[]fallbackdomain.PublishFile) error {
	return normalizeStoreError(b.db.Create(files).Error)
}
func (b *backendStore) CreatePublishRedirects(redirects *[]fallbackdomain.PublishRedirect) error {
	return normalizeStoreError(b.db.Create(redirects).Error)
}
func (b *backendStore) DeletePublishFiles(ids []uint) error {
	return normalizeStoreError(b.db.Where("publish_id IN ?", ids).Delete(&fallbackdomain.PublishFile{}).Error)
}
func (b *backendStore) DeletePublishRedirects(ids []uint) error {
	return normalizeStoreError(b.db.Where("publish_id IN ?", ids).Delete(&fallbackdomain.PublishRedirect{}).Error)
}
func (b *backendStore) DeleteInactivePublishes(siteID uint, ids []uint) error {
	return normalizeStoreError(b.db.Where("site_id = ? AND id IN ? AND active = ?", siteID, ids, false).Delete(&fallbackdomain.Publish{}).Error)
}
func (b *backendStore) PublishedSiteConflict(siteID uint, out *fallbackdomain.Site) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.Site{}).Select("fallback_html_sites.*").Joins("JOIN fallback_html_publishes ON fallback_html_publishes.site_id = fallback_html_sites.id").Where("fallback_html_sites.id <> ? AND fallback_html_sites.enabled = ? AND fallback_html_publishes.active = ?", siteID, true, true).Order("fallback_html_publishes.id DESC").First(out).Error)
}
func (b *backendStore) CreateSafetyReport(report *fallbackdomain.SafetyReport) error {
	return normalizeStoreError(b.db.Create(report).Error)
}
func (b *backendStore) CreateEvent(event *fallbackdomain.Event) error {
	return normalizeStoreError(b.db.Create(event).Error)
}
func (b *backendStore) LoadTemplateSource(templateID string, out *fallbackdomain.TemplateSource) error {
	return normalizeStoreError(b.db.Where("template_id = ? AND manifest_json <> ''", templateID).First(out).Error)
}
func (b *backendStore) ListTemplateSources(out *[]fallbackdomain.TemplateSource) error {
	return normalizeStoreError(b.db.Where("manifest_json <> ''").Order("template_id ASC").Find(out).Error)
}
func (b *backendStore) UpsertTemplateSource(values fallbackdomain.TemplateSource, createdAt int64, out *fallbackdomain.TemplateSource) error {
	return normalizeStoreError(b.db.Where("template_id = ?", values.TemplateID).Assign(values).Attrs(fallbackdomain.TemplateSource{CreatedAt: createdAt}).FirstOrCreate(out).Error)
}
func (b *backendStore) DeleteTemplateSource(source *fallbackdomain.TemplateSource) error {
	return normalizeStoreError(b.db.Delete(source).Error)
}
func (b *backendStore) ResetTemplateSource(id uint, definition fallbackdomain.TemplateDefinition, now int64) error {
	return normalizeStoreError(b.db.Model(&fallbackdomain.TemplateSource{}).Where("id = ?", id).Updates(map[string]any{"name": definition.Name, "source": definition.Source, "license": definition.License, "content_type_profile": definition.ContentTypeProfile, "catalog_url": "", "manifest_url": "", "manifest_json": nil, "installed": true, "updated_at": now}).Error)
}
func (b *backendStore) LoadTarget(siteID, id uint, out *fallbackdomain.RuntimeTarget) error {
	return normalizeStoreError(b.db.Where("site_id = ?", siteID).First(out, id).Error)
}
func (b *backendStore) SaveTarget(target *fallbackdomain.RuntimeTarget) error {
	return normalizeStoreError(b.db.Save(target).Error)
}
func (b *backendStore) CreateTarget(target *fallbackdomain.RuntimeTarget) error {
	return normalizeStoreError(b.db.Create(target).Error)
}
func (b *backendStore) DeleteTarget(target *fallbackdomain.RuntimeTarget) error {
	return normalizeStoreError(b.db.Delete(target).Error)
}
func (b *backendStore) DeleteOtherTargets(siteID, exceptID uint) error {
	return normalizeStoreError(b.db.Where("site_id = ? AND id <> ?", siteID, exceptID).Delete(&fallbackdomain.RuntimeTarget{}).Error)
}
func (b *backendStore) ListSavedTargets(out *[]fallbackdomain.RuntimeTarget) error {
	return normalizeStoreError(b.db.Order("site_id ASC, id ASC").Find(out).Error)
}
func (b *backendStore) SetSitePublication(siteID uint, status string, clearError bool, now int64) error {
	values := map[string]any{"status": status, "updated_at": now}
	if clearError {
		values["last_error"] = ""
	}
	return normalizeStoreError(b.db.Model(&fallbackdomain.Site{}).Where("id = ?", siteID).Updates(values).Error)
}
func (b *backendStore) LoadRollbackPublish(siteID uint, version string, out *fallbackdomain.Publish) error {
	query := b.db.Preload("Files").Preload("Redirects").Where("site_id = ?", siteID)
	if version != "" {
		query = query.Where("version = ?", version)
	} else {
		query = query.Where("active = ?", false)
	}
	return normalizeStoreError(query.Order("id DESC").First(out).Error)
}

func (b *backendStore) LoadActivePublish(out *fallbackdomain.Publish) error {
	return normalizeStoreError(b.db.Preload("Files").Preload("Redirects").Joins("JOIN fallback_html_sites ON fallback_html_sites.id = fallback_html_publishes.site_id").Where("fallback_html_publishes.active = ? AND fallback_html_sites.enabled = ?", true, true).Order("fallback_html_publishes.id DESC").First(out).Error)
}
func (b *backendStore) AllowedExternalResources(siteID uint, out *[]fallbackdomain.ExternalResource) error {
	return normalizeStoreError(b.db.Where("site_id = ? AND allowed = ?", siteID, true).Find(out).Error)
}
func (b *backendStore) StandaloneTargets(siteID uint, out *[]fallbackdomain.RuntimeTarget) error {
	return normalizeStoreError(b.db.Where("site_id = ? AND kind = ?", siteID, "standalone").Find(out).Error)
}

func (b *backendStore) PrepareHTTPBinding(siteID uint, revision string, targets []fallbackdomain.RuntimeTarget, generation uint64) (PublicationBinding, error) {
	return b.runtime.prepareHTTPBindingAt(b.db, siteID, revision, targets, generation)
}

// LoadPublication reads only the database generation supplied by the existing
// owner. Rehearsal never calls this live-serving composition.
func (b *backendStore) LoadPublication(ctx context.Context, generation uint64) (*PublicationCandidate, error) {
	scoped := &backendStore{db: b.db.WithContext(ctx), runtime: b.runtime}
	var publish fallbackdomain.Publish
	if err := scoped.LoadActivePublish(&publish); err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	candidate := &PublicationCandidate{Publish: publish}
	if err := scoped.AllowedExternalResources(publish.SiteID, &candidate.Resources); err != nil {
		return nil, err
	}
	for _, file := range publish.Files {
		data, err := (backendContent{}).ReadPublishFile(publish, file)
		if err != nil {
			return nil, err
		}
		candidate.Files = append(candidate.Files, data)
	}
	if err := scoped.StandaloneTargets(publish.SiteID, &candidate.Targets); err != nil {
		return nil, err
	}
	var err error
	candidate.Binding, err = b.runtime.prepareHTTPBindingAt(scoped.db, publish.SiteID, publish.Version, candidate.Targets, generation)
	return candidate, err
}
