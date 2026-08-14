//go:build !minimal

package domain

import "encoding/json"

type Site struct {
	ID           uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled" gorm:"not null;default:true"`
	TemplateID   string `json:"templateId" gorm:"column:template_id"`
	ExposureMode string `json:"exposureMode" gorm:"column:exposure_mode;default:direct;not null"`
	Hostname     string `json:"hostname"`
	Status       string `json:"status" gorm:"default:draft;not null"`
	LastError    string `json:"lastError" gorm:"column:last_error"`
	CreatedAt    int64  `json:"createdAt" gorm:"default:0;not null"`
	UpdatedAt    int64  `json:"updatedAt" gorm:"default:0;not null"`

	Pages     []Page          `json:"pages,omitempty" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
	Redirects []Redirect      `json:"redirects,omitempty" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
	Assets    []Asset         `json:"assets,omitempty" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
	Targets   []RuntimeTarget `json:"targets,omitempty" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
	Publishes []Publish       `json:"publishes,omitempty" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
}

func (Site) TableName() string { return "fallback_html_sites" }

type Page struct {
	ID            uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID        uint   `json:"siteId" gorm:"column:site_id;index:idx_fallback_html_page_path,unique;not null"`
	Path          string `json:"path"`
	CanonicalPath string `json:"canonicalPath" gorm:"column:canonical_path;index:idx_fallback_html_page_path,unique;not null"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	ContentMode   string `json:"contentMode" gorm:"column:content_mode;default:text;not null"`
	IsHome        bool   `json:"isHome" gorm:"column:is_home;default:false;not null"`
	SortOrder     int    `json:"sortOrder" gorm:"column:sort_order;default:0;not null"`
	Provenance    string `json:"provenance" gorm:"default:generated;not null"`
	CreatedAt     int64  `json:"createdAt" gorm:"default:0;not null"`
	UpdatedAt     int64  `json:"updatedAt" gorm:"default:0;not null"`
}

func (Page) TableName() string { return "fallback_html_pages" }

type Redirect struct {
	ID         uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID     uint   `json:"siteId" gorm:"column:site_id;index:idx_fallback_html_redirect_from,unique;not null"`
	FromPath   string `json:"fromPath" gorm:"column:from_path;index:idx_fallback_html_redirect_from,unique;not null"`
	ToPath     string `json:"toPath" gorm:"column:to_path;not null"`
	StatusCode int    `json:"statusCode" gorm:"column:status_code;default:302;not null"`
	External   bool   `json:"external" gorm:"default:false;not null"`
	CreatedAt  int64  `json:"createdAt" gorm:"default:0;not null"`
	UpdatedAt  int64  `json:"updatedAt" gorm:"default:0;not null"`
}

func (Redirect) TableName() string { return "fallback_html_redirects" }

type Asset struct {
	ID          uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID      uint   `json:"siteId" gorm:"column:site_id;index;not null"`
	LogicalPath string `json:"logicalPath" gorm:"column:logical_path;not null"`
	FilePath    string `json:"filePath" gorm:"column:file_path;not null"`
	MimeType    string `json:"mimeType" gorm:"column:mime_type;not null"`
	Sha256      string `json:"sha256" gorm:"column:sha256;not null"`
	SizeBytes   int64  `json:"sizeBytes" gorm:"column:size_bytes;default:0;not null"`
	Provenance  string `json:"provenance" gorm:"default:uploaded;not null"`
	CreatedAt   int64  `json:"createdAt" gorm:"default:0;not null"`
}

func (Asset) TableName() string { return "fallback_html_assets" }

type Publish struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID    uint   `json:"siteId" gorm:"column:site_id;index;not null"`
	Version   string `json:"version" gorm:"index;not null"`
	RootDir   string `json:"rootDir" gorm:"column:root_dir;not null"`
	Active    bool   `json:"active" gorm:"index;default:false;not null"`
	CreatedAt int64  `json:"createdAt" gorm:"default:0;not null"`

	Files     []PublishFile     `json:"files,omitempty" gorm:"foreignKey:PublishID;constraint:OnDelete:CASCADE"`
	Redirects []PublishRedirect `json:"redirects,omitempty" gorm:"foreignKey:PublishID;constraint:OnDelete:CASCADE"`
}

func (Publish) TableName() string { return "fallback_html_publishes" }

type PublishFile struct {
	ID          uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	PublishID   uint   `json:"publishId" gorm:"column:publish_id;index;not null"`
	PublicPath  string `json:"publicPath" gorm:"column:public_path;index;not null"`
	FilePath    string `json:"filePath" gorm:"column:file_path;not null"`
	MimeType    string `json:"mimeType" gorm:"column:mime_type;not null"`
	Sha256      string `json:"sha256" gorm:"column:sha256;not null"`
	SizeBytes   int64  `json:"sizeBytes" gorm:"column:size_bytes;default:0;not null"`
	CachePolicy string `json:"cachePolicy" gorm:"column:cache_policy"`
}

func (PublishFile) TableName() string { return "fallback_html_publish_files" }

type PublishRedirect struct {
	ID         uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	PublishID  uint   `json:"publishId" gorm:"column:publish_id;index:idx_fallback_html_publish_redirect_from,unique;not null"`
	FromPath   string `json:"fromPath" gorm:"column:from_path;index:idx_fallback_html_publish_redirect_from,unique;not null"`
	ToPath     string `json:"toPath" gorm:"column:to_path;not null"`
	StatusCode int    `json:"statusCode" gorm:"column:status_code;default:302;not null"`
	External   bool   `json:"external" gorm:"default:false;not null"`
}

func (PublishRedirect) TableName() string { return "fallback_html_publish_redirects" }

type SafetyReport struct {
	ID        uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID    uint            `json:"siteId" gorm:"column:site_id;index;not null"`
	OK        bool            `json:"ok" gorm:"not null"`
	Warnings  json.RawMessage `json:"warnings"`
	CreatedAt int64           `json:"createdAt" gorm:"default:0;not null"`
}

func (SafetyReport) TableName() string { return "fallback_html_safety_reports" }

type TemplateSource struct {
	ID                 uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	TemplateID         string          `json:"templateId" gorm:"column:template_id;uniqueIndex;not null"`
	Name               string          `json:"name" gorm:"not null"`
	Source             string          `json:"source"`
	License            string          `json:"license"`
	ContentTypeProfile string          `json:"contentTypeProfile" gorm:"column:content_type_profile"`
	CatalogURL         string          `json:"catalogUrl" gorm:"column:catalog_url"`
	ManifestURL        string          `json:"manifestUrl" gorm:"column:manifest_url"`
	ManifestJSON       json.RawMessage `json:"manifestJson" gorm:"column:manifest_json"`
	Installed          bool            `json:"installed" gorm:"default:false;not null"`
	CreatedAt          int64           `json:"createdAt" gorm:"default:0;not null"`
	UpdatedAt          int64           `json:"updatedAt" gorm:"default:0;not null"`
}

func (TemplateSource) TableName() string { return "fallback_html_template_sources" }

type SelfStealDraft struct {
	ID          uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID      uint            `json:"siteId" gorm:"column:site_id;index;not null"`
	CoreDraftID uint            `json:"coreDraftId" gorm:"column:core_draft_id;index;default:0;not null"`
	Status      string          `json:"status" gorm:"not null"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   int64           `json:"createdAt" gorm:"default:0;not null"`
}

func (SelfStealDraft) TableName() string { return "fallback_html_self_steal_drafts" }

type RuntimeTarget struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID    uint   `json:"siteId" gorm:"column:site_id;index;not null"`
	Kind      string `json:"kind" gorm:"default:web;not null"`
	Host      string `json:"host"`
	Listen    string `json:"listen"`
	Port      int    `json:"port" gorm:"default:0;not null"`
	RootPath  string `json:"rootPath" gorm:"column:root_path;default:/;not null"`
	Runtime   string `json:"runtime" gorm:"default:gin;not null"`
	TLS       bool   `json:"tls" gorm:"default:false;not null"`
	CreatedAt int64  `json:"createdAt" gorm:"default:0;not null"`
	UpdatedAt int64  `json:"updatedAt" gorm:"default:0;not null"`
}

func (RuntimeTarget) TableName() string { return "fallback_html_runtime_targets" }

type ExternalResource struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID    uint   `json:"siteId" gorm:"column:site_id;index;not null"`
	Kind      string `json:"kind" gorm:"not null"`
	URL       string `json:"url" gorm:"not null"`
	Allowed   bool   `json:"allowed" gorm:"default:false;not null"`
	CreatedAt int64  `json:"createdAt" gorm:"default:0;not null"`
}

func (ExternalResource) TableName() string { return "fallback_html_external_resources" }

type Event struct {
	ID        uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID    uint            `json:"siteId" gorm:"column:site_id;index"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action" gorm:"not null"`
	Details   json.RawMessage `json:"details"`
	CreatedAt int64           `json:"createdAt" gorm:"default:0;not null"`
}

func (Event) TableName() string { return "fallback_html_events" }
