//go:build !minimal

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
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
	listeners  map[string]*runtimeListener
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

type standaloneTargetConfig struct {
	key         string
	address     string
	tls         bool
	certFile    string
	keyFile     string
	tlsRevision string
	certificate *tls.Certificate
}

type runtimeListener struct {
	config standaloneTargetConfig
	server *http.Server
}

func NewRuntime() *Runtime {
	r := &Runtime{limiter: newPublicSiteLimiter(), listeners: map[string]*runtimeListener{}}
	r.snapshot.Store((*snapshot)(nil))
	return r
}

func (r *Runtime) Start(db *gorm.DB) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
	if r.unregister == nil {
		unregister, err := publicsurface.Register("fallback-public-site", r)
		if err != nil {
			return err
		}
		r.unregister = unregister
	}
	if err := r.rebuildLocked(); err != nil {
		r.unregister()
		r.unregister = nil
		r.db = nil
		return err
	}
	return nil
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
	r.stopStandaloneLocked()
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
		r.stopStandaloneLocked()
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
		r.stopStandaloneLocked()
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
	targets, err := r.activeStandaloneTargets(publish.SiteID)
	if err != nil {
		return err
	}
	if err := r.applyStandaloneTargetsLocked(targets); err != nil {
		return err
	}
	r.snapshot.Store(next)
	return nil
}

func (r *Runtime) Owns(listen string, port int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.listeners[runtimeTargetKey(listen, port)]
	return ok
}

func (r *Runtime) activeStandaloneTargets(siteID uint) ([]fallbackdomain.RuntimeTarget, error) {
	var targets []fallbackdomain.RuntimeTarget
	if err := r.db.Where("site_id = ? AND kind = ?", siteID, "standalone").Find(&targets).Error; err != nil {
		return nil, err
	}
	return targets, nil
}

func (r *Runtime) applyStandaloneTargetsLocked(targets []fallbackdomain.RuntimeTarget) error {
	desired := make(map[string]standaloneTargetConfig, len(targets))
	for _, target := range targets {
		config, ok, err := standaloneConfigForTarget(target)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, duplicate := desired[config.key]; duplicate {
			return fmt.Errorf("fallback-html runtime target %s is duplicated", config.address)
		}
		desired[config.key] = config
	}

	next := make(map[string]*runtimeListener, len(desired))
	started := make([]*runtimeListener, 0, len(desired))
	closed := make(map[string]*runtimeListener)
	rollback := func(cause error) error {
		for _, state := range started {
			_ = state.server.Close()
		}
		restored := make(map[string]*runtimeListener, len(r.listeners))
		for key, state := range r.listeners {
			restored[key] = state
		}
		var restoreErr error
		for key, state := range closed {
			replacement, err := r.startStandaloneLocked(state.config)
			if err != nil {
				delete(restored, key)
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore listener %s: %w", state.config.address, err))
				continue
			}
			restored[key] = replacement
		}
		r.listeners = restored
		return errors.Join(cause, restoreErr)
	}

	for key, config := range desired {
		current := r.listeners[key]
		if current != nil && sameStandaloneConfig(current.config, config) {
			next[key] = current
			continue
		}
		if current != nil {
			continue
		}
		state, err := r.startStandaloneLocked(config)
		if err != nil {
			return rollback(fmt.Errorf("fallback-html cannot publish on %s: %w", config.address, err))
		}
		started = append(started, state)
		next[key] = state
	}

	for key, config := range desired {
		current := r.listeners[key]
		if current == nil || sameStandaloneConfig(current.config, config) {
			continue
		}
		_ = current.server.Close()
		closed[key] = current
		state, err := r.startStandaloneLocked(config)
		if err != nil {
			return rollback(fmt.Errorf("fallback-html cannot replace listener %s: %w", config.address, err))
		}
		started = append(started, state)
		next[key] = state
	}

	for key, state := range r.listeners {
		if next[key] != state {
			_ = state.server.Close()
		}
	}
	r.listeners = next
	return nil
}

func standaloneConfigForTarget(target fallbackdomain.RuntimeTarget) (standaloneTargetConfig, bool, error) {
	if target.Port <= 0 {
		return standaloneTargetConfig{}, false, nil
	}
	listen := strings.TrimSpace(target.Listen)
	if listen == "" {
		listen = "127.0.0.1"
	}
	config := standaloneTargetConfig{
		key:     runtimeTargetKey(listen, target.Port),
		address: net.JoinHostPort(listen, strconv.Itoa(target.Port)),
		tls:     target.TLS,
	}
	if !target.TLS {
		return config, true, nil
	}
	config.certFile, config.keyFile = runtimeTLSFiles()
	if config.certFile == "" || config.keyFile == "" {
		return standaloneTargetConfig{}, false, fmt.Errorf("fallback-html cannot publish TLS target %s: panel certificate and key are required", config.address)
	}
	certificate, revision, err := loadRuntimeTLSMaterial(config.certFile, config.keyFile)
	if err != nil {
		return standaloneTargetConfig{}, false, fmt.Errorf("fallback-html TLS target %s is invalid: %w", config.address, err)
	}
	config.certificate = &certificate
	config.tlsRevision = revision
	return config, true, nil
}

func loadRuntimeTLSMaterial(certFile, keyFile string) (tls.Certificate, string, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certInfo, err := os.Stat(certFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	revision := fmt.Sprintf("%d:%d:%d:%d", certInfo.Size(), certInfo.ModTime().UnixNano(), keyInfo.Size(), keyInfo.ModTime().UnixNano())
	return certificate, revision, nil
}

func sameStandaloneConfig(left, right standaloneTargetConfig) bool {
	return left.key == right.key && left.address == right.address && left.tls == right.tls &&
		left.certFile == right.certFile && left.keyFile == right.keyFile && left.tlsRevision == right.tlsRevision
}

func (r *Runtime) startStandaloneLocked(config standaloneTargetConfig) (*runtimeListener, error) {
	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Addr:              config.address,
		Handler:           http.HandlerFunc(r.serveHTTPPublic),
		ReadHeaderTimeout: 5 * time.Second,
	}
	state := &runtimeListener{config: config, server: server}
	go func() {
		var serveErr error
		if config.tls {
			server.TLSConfig = &tls.Config{
				Certificates: []tls.Certificate{*config.certificate},
				MinVersion:   tls.VersionTLS12,
			}
			serveErr = server.ServeTLS(listener, "", "")
		} else {
			serveErr = server.Serve(listener)
		}
		if serveErr == nil || errors.Is(serveErr, http.ErrServerClosed) {
			return
		}
		r.mu.Lock()
		if r.listeners[config.key] == state {
			delete(r.listeners, config.key)
		}
		r.mu.Unlock()
	}()
	return state, nil
}

func (r *Runtime) stopStandaloneLocked() {
	if len(r.listeners) == 0 {
		r.listeners = map[string]*runtimeListener{}
		return
	}
	for _, state := range r.listeners {
		_ = state.server.Close()
	}
	r.listeners = map[string]*runtimeListener{}
}

func runtimeTLSFiles() (string, string) {
	settings := coreservice.SettingService{}
	certFile, certErr := settings.GetCertFile()
	keyFile, keyErr := settings.GetKeyFile()
	if certErr != nil || keyErr != nil {
		return "", ""
	}
	return strings.TrimSpace(certFile), strings.TrimSpace(keyFile)
}

func runtimeTargetKey(listen string, port int) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = "127.0.0.1"
	}
	return net.JoinHostPort(listen, strconv.Itoa(port))
}

func (r *Runtime) serveHTTPPublic(w http.ResponseWriter, req *http.Request) {
	snap := r.snapshot.Load().(*snapshot)
	if snap == nil {
		http.NotFound(w, req)
		return
	}
	started := time.Now()
	observed := &observationResponseWriter{ResponseWriter: w}
	r.serveHTTPPublicResponse(observed, req, snap)
	emitHTTPObservation(req, observed.statusCode(), observed.bytes, snap.siteID, time.Since(started))
}

func (r *Runtime) serveHTTPPublicResponse(w http.ResponseWriter, req *http.Request, snap *snapshot) {
	if !r.allowHTTPPublicRequest(w, req, snap.csp) {
		return
	}
	requestPath := req.URL.Path
	if requestPath == "" {
		requestPath = "/"
	}
	path, err := fallbackdomain.NormalizePublicPath(requestPath)
	if err != nil || fallbackdomain.IsReservedPublicPath(path, nil) {
		http.NotFound(w, req)
		return
	}
	if req.Method == http.MethodPost && path == "/" {
		if file, ok := snap.pages["/"]; ok {
			r.writeHTTPFile(w, req, file, http.StatusOK, snap.csp)
			return
		}
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if redirect, ok := snap.redirects[path]; ok {
		setPublicHTTPHeaders(w, snap.csp)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, req, redirect.toPath, redirect.statusCode)
		return
	}
	if requestPath != path {
		if _, ok := snap.pages[path]; ok {
			setPublicHTTPHeaders(w, snap.csp)
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, req, path, http.StatusPermanentRedirect)
			return
		}
	}
	file, ok := snap.pages[path]
	if !ok {
		if !snap.hasNotFound {
			http.NotFound(w, req)
			return
		}
		r.writeHTTPFile(w, req, snap.notFound, http.StatusNotFound, snap.csp)
		return
	}
	r.writeHTTPFile(w, req, file, http.StatusOK, snap.csp)
}

func (r *Runtime) allowHTTPPublicRequest(w http.ResponseWriter, req *http.Request, csp string) bool {
	if r.limiter == nil {
		return true
	}
	key := req.RemoteAddr
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil && host != "" {
		key = host
	}
	if key == "" {
		key = "unknown"
	}
	decision := r.limiter.Allow(key)
	if decision.Allowed {
		return true
	}
	setPublicHTTPHeaders(w, csp)
	retryAfter := int((decision.RetryAfter + time.Second - 1) / time.Second)
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.WriteHeader(http.StatusTooManyRequests)
	return false
}

func (r *Runtime) writeHTTPFile(w http.ResponseWriter, req *http.Request, file publishedFile, status int, csp string) {
	setPublicHTTPHeaders(w, csp)
	if file.cache != "" {
		w.Header().Set("Cache-Control", file.cache)
	} else {
		w.Header().Set("Cache-Control", htmlCachePolicy)
	}
	etag := `"` + file.sha256 + `"`
	w.Header().Set("ETag", etag)
	if file.modified > 0 {
		w.Header().Set("Last-Modified", time.Unix(file.modified, 0).UTC().Format(http.TimeFormat))
	}
	if status == http.StatusOK && requestCacheFresh(req, etag, file.modified) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", file.mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(file.data)))
	w.WriteHeader(status)
	if req.Method != http.MethodHead {
		_, _ = w.Write(file.data)
	}
}

func setPublicHTTPHeaders(w http.ResponseWriter, csp string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Del("X-Frame-Options")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Security-Policy", csp)
}

func (r *Runtime) ServePublic(c *gin.Context, ctx publicsurface.Context) bool {
	snap := r.snapshot.Load().(*snapshot)
	started := time.Now()
	handled := r.servePublic(c, ctx, true)
	if handled && snap != nil {
		emitGinObservation(c, snap.siteID, time.Since(started))
	}
	return handled
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
	if c.Request.Method == http.MethodPost && path == "/" {
		r.serveFile(c, file, http.StatusOK, snap.csp)
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
		"; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'none'; frame-src 'none'; form-action 'none'"
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

type observationResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *observationResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *observationResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *observationResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func emitGinObservation(c *gin.Context, siteID uint, duration time.Duration) {
	if c == nil || c.Request == nil {
		return
	}
	emitPublicObservation(c.Request, c.ClientIP(), c.Writer.Status(), int64(c.Writer.Size()), siteID, duration)
}

func emitHTTPObservation(req *http.Request, status int, bytes int64, siteID uint, duration time.Duration) {
	if req == nil {
		return
	}
	sourceIP := req.RemoteAddr
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		sourceIP = host
	}
	emitPublicObservation(req, sourceIP, status, bytes, siteID, duration)
}

func emitPublicObservation(req *http.Request, sourceIP string, status int, bytes int64, siteID uint, duration time.Duration) {
	if publicsurface.ObservationSubscribers() == 0 {
		return
	}
	requestURI := req.URL.RequestURI()
	publicsurface.EmitObservation(publicsurface.Observation{
		ResourceID:     "component:fallback-html:site:" + strconv.FormatUint(uint64(siteID), 10),
		ResourceKind:   "public_site",
		ComponentID:    "fallback-html",
		SourceIP:       strings.TrimSpace(sourceIP),
		MethodClass:    publicsurface.ClassifyMethod(req.Method),
		PathClass:      publicsurface.ClassifyPath(requestURI, false),
		StatusClass:    publicsurface.ClassifyStatus(status),
		UserAgentClass: publicsurface.ClassifyUserAgent(req.UserAgent()),
		BytesClass:     publicsurface.ClassifyBytes(bytes),
		DurationClass:  publicsurface.ClassifyDuration(duration.Milliseconds()),
		RateLimited:    status == http.StatusTooManyRequests,
		ObservedAt:     time.Now().Unix(),
	})
}
