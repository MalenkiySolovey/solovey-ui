//go:build !minimal

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
	"github.com/MalenkiySolovey/solovey-ui/web/publicsurface"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var DefaultRuntime = NewRuntime()

const (
	publicSiteRateWindow = time.Minute
	publicSiteRateLimit  = 240
	publicSiteRateMaxKey = 8192
)

type Runtime struct {
	mu         sync.Mutex
	db         *gorm.DB
	unregister func()
	limiter    *ratelimit.FixedWindow[string]
	snapshot   atomic.Value // *snapshot
}

type RuntimeStatus struct {
	Active    bool `json:"active"`
	SiteID    uint `json:"siteId,omitempty"`
	Pages     int  `json:"pages"`
	Redirects int  `json:"redirects"`
}

type snapshot struct {
	siteID      uint
	pages       map[string]publishedFile
	redirects   map[string]publishedRedirect
	csp         string
	notFound    publishedFile
	hasNotFound bool
}

type publishedFile struct {
	publicPath string
	filePath   string
	mimeType   string
	sha256     string
	cache      string
	modified   int64
	data       []byte
}

type publishedRedirect struct {
	fromPath   string
	toPath     string
	statusCode int
	external   bool
}

func NewRuntime() *Runtime {
	r := &Runtime{limiter: newPublicSiteLimiter()}
	r.snapshot.Store((*snapshot)(nil))
	return r
}

func (r *Runtime) Start(db *gorm.DB) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
	if r.unregister == nil {
		r.unregister = publicsurface.Register("fallback-public-site", r)
	}
	return r.rebuildLocked()
}

func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unregister != nil {
		r.unregister()
		r.unregister = nil
	}
	r.db = nil
	if r.limiter != nil {
		r.limiter.ResetAll()
	}
	r.snapshot.Store((*snapshot)(nil))
}

func (r *Runtime) Rebuild(db *gorm.DB) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db != nil {
		r.db = db
	}
	return r.rebuildLocked()
}

func (r *Runtime) Status() RuntimeStatus {
	snap := r.snapshot.Load().(*snapshot)
	if snap == nil {
		return RuntimeStatus{}
	}
	return RuntimeStatus{
		Active:    true,
		SiteID:    snap.siteID,
		Pages:     len(snap.pages),
		Redirects: len(snap.redirects),
	}
}

func (r *Runtime) rebuildLocked() error {
	if r.db == nil {
		r.snapshot.Store((*snapshot)(nil))
		return nil
	}
	var publish fallbackdomain.Publish
	err := r.db.
		Preload("Files").
		Preload("Redirects").
		Joins("JOIN fallback_html_sites ON fallback_html_sites.id = fallback_html_publishes.site_id").
		Where("fallback_html_publishes.active = ? AND fallback_html_sites.enabled = ?", true, true).
		Order("fallback_html_publishes.id DESC").
		First(&publish).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		r.snapshot.Store((*snapshot)(nil))
		return nil
	}
	if err != nil {
		return err
	}
	next := &snapshot{
		siteID:    publish.SiteID,
		pages:     make(map[string]publishedFile, len(publish.Files)),
		redirects: make(map[string]publishedRedirect, len(publish.Redirects)),
	}
	var resources []fallbackdomain.ExternalResource
	if err := r.db.Where("site_id = ? AND allowed = ?", publish.SiteID, true).Find(&resources).Error; err != nil {
		return err
	}
	next.csp = publicSiteCSP(resources)
	for _, file := range publish.Files {
		data, err := readOwnedRegularFile(publish.RootDir, file.FilePath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.Sha256 {
			return fmt.Errorf("fallback-html publish file %s failed integrity check", file.PublicPath)
		}
		item := publishedFile{
			publicPath: file.PublicPath,
			filePath:   file.FilePath,
			mimeType:   file.MimeType,
			sha256:     file.Sha256,
			cache:      file.CachePolicy,
			modified:   publish.CreatedAt,
			data:       data,
		}
		if file.PublicPath == "/404.html" {
			next.notFound = item
			next.hasNotFound = true
			continue
		}
		next.pages[file.PublicPath] = item
	}
	for _, redirect := range publish.Redirects {
		next.redirects[redirect.FromPath] = publishedRedirect{
			fromPath:   redirect.FromPath,
			toPath:     redirect.ToPath,
			statusCode: redirect.StatusCode,
			external:   redirect.External,
		}
	}
	r.snapshot.Store(next)
	return nil
}

func (r *Runtime) ServePublic(c *gin.Context, ctx publicsurface.Context) bool {
	return r.servePublic(c, ctx, true)
}

func (r *Runtime) servePublic(c *gin.Context, ctx publicsurface.Context, enforceRateLimit bool) bool {
	snap := r.snapshot.Load().(*snapshot)
	if snap == nil {
		return false
	}
	requestPath := c.Request.URL.Path
	if requestPath == "" {
		requestPath = "/"
	}
	path, err := fallbackdomain.NormalizePublicPath(requestPath)
	if err != nil || fallbackdomain.IsReservedPublicPath(path, []string{ctx.AdminBasePath}) {
		return false
	}
	if requestPath != path {
		if !r.allowPublicRequestIfNeeded(c, snap.csp, enforceRateLimit) {
			return true
		}
		if redirect, ok := snap.redirects[path]; ok {
			r.redirect(c, redirect.toPath, redirect.statusCode, snap.csp)
			return true
		}
		if _, ok := snap.pages[path]; ok {
			r.redirect(c, path, http.StatusPermanentRedirect, snap.csp)
			return true
		}
	}
	file, ok := snap.pages[path]
	if !r.allowPublicRequestIfNeeded(c, snap.csp, enforceRateLimit) {
		return true
	}
	if !ok {
		if redirect, redirectOK := snap.redirects[path]; redirectOK {
			r.redirect(c, redirect.toPath, redirect.statusCode, snap.csp)
			return true
		}
		if !snap.hasNotFound {
			return false
		}
		r.serveFile(c, snap.notFound, http.StatusNotFound, snap.csp)
		return true
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Status(http.StatusMethodNotAllowed)
		return true
	}
	r.serveFile(c, file, http.StatusOK, snap.csp)
	return true
}

func (r *Runtime) allowPublicRequestIfNeeded(c *gin.Context, csp string, enforce bool) bool {
	if !enforce {
		return true
	}
	return r.allowPublicRequest(c, csp)
}

func (r *Runtime) allowPublicRequest(c *gin.Context, csp string) bool {
	if r.limiter == nil {
		return true
	}
	key := c.ClientIP()
	if key == "" {
		key = "unknown"
	}
	decision := r.limiter.Allow(key)
	if decision.Allowed {
		return true
	}
	securitymiddleware.SetPublicSiteHeadersWithCSP(c, csp)
	retryAfter := int((decision.RetryAfter + time.Second - 1) / time.Second)
	if retryAfter < 1 {
		retryAfter = 1
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.AbortWithStatus(http.StatusTooManyRequests)
	return false
}

func newPublicSiteLimiter() *ratelimit.FixedWindow[string] {
	return ratelimit.NewFixedWindow[string](publicSiteRateWindow, publicSiteRateLimit, publicSiteRateMaxKey, 0)
}

func (r *Runtime) redirect(c *gin.Context, target string, status int, csp string) {
	securitymiddleware.SetPublicSiteHeadersWithCSP(c, csp)
	c.Header("Cache-Control", "no-store")
	c.Redirect(status, target)
}

func (r *Runtime) serveFile(c *gin.Context, file publishedFile, status int, csp string) {
	securitymiddleware.SetPublicSiteHeadersWithCSP(c, csp)
	if cache := file.cache; cache != "" {
		c.Header("Cache-Control", cache)
	} else {
		c.Header("Cache-Control", htmlCachePolicy)
	}
	etag := `"` + file.sha256 + `"`
	c.Header("ETag", etag)
	if file.modified > 0 {
		c.Header("Last-Modified", time.Unix(file.modified, 0).UTC().Format(http.TimeFormat))
	}
	if status == http.StatusOK && requestCacheFresh(c.Request, etag, file.modified) {
		c.AbortWithStatus(http.StatusNotModified)
		return
	}
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", file.mimeType)
		c.Header("Content-Length", strconv.Itoa(len(file.data)))
		c.Status(status)
		c.Writer.WriteHeaderNow()
		return
	}
	c.DataFromReader(status, int64(len(file.data)), file.mimeType, bytes.NewReader(file.data), nil)
}

func requestCacheFresh(req *http.Request, etag string, modified int64) bool {
	if req == nil {
		return false
	}
	if match := strings.TrimSpace(req.Header.Get("If-None-Match")); match != "" {
		for _, candidate := range strings.Split(match, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || candidate == etag {
				return true
			}
		}
	}
	if modified <= 0 {
		return false
	}
	value := strings.TrimSpace(req.Header.Get("If-Modified-Since"))
	if value == "" {
		return false
	}
	since, err := http.ParseTime(value)
	if err != nil {
		return false
	}
	return !time.Unix(modified, 0).UTC().After(since.UTC())
}

func publicSiteCSP(resources []fallbackdomain.ExternalResource) string {
	imgSrc := []string{"'self'", "data:"}
	fontSrc := []string{"'self'", "data:"}
	for _, resource := range resources {
		origin := externalOrigin(resource.URL)
		if origin == "" {
			continue
		}
		switch resource.Kind {
		case "image":
			imgSrc = appendUnique(imgSrc, origin)
		case "font":
			fontSrc = appendUnique(fontSrc, origin)
		}
	}
	return "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; img-src " +
		strings.Join(imgSrc, " ") +
		"; font-src " + strings.Join(fontSrc, " ") +
		"; style-src 'self' 'unsafe-inline'; script-src 'none'; connect-src 'none'; frame-src 'none'; form-action 'self'"
}

func externalOrigin(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.Stop()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
