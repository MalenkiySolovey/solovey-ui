package web

import (
	"context"
	"crypto/tls"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/api"
	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/httpconn"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	domainmiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/domain"
	requestbudget "github.com/MalenkiySolovey/solovey-ui/middleware/requestbudget"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/network/autohttps"
	"github.com/MalenkiySolovey/solovey-ui/network/bind"
	"github.com/MalenkiySolovey/solovey-ui/service"
	pressureService "github.com/MalenkiySolovey/solovey-ui/service/resourcepressure"

	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// all: is required so Go embeds asset files whose names begin with "_" or ".".
// Rolldown hashes are URL-safe base64 and occasionally produce "_"-leading
// chunk/style names; a plain `//go:embed *` silently drops them, yielding a 404
// on a dynamically imported module and a blank panel. (Vite is also configured
// to prefix asset names so they never start with "_".)
//
//go:embed all:*
var content embed.FS

type Server struct {
	httpServer       *http.Server
	listener         net.Listener
	settingService   service.SettingService
	runtime          *service.Runtime
	assetsFS         fs.FS
	unregisterHealth func()
	running          atomic.Bool
}

type Option func(*Server)

func WithRuntime(runtime *service.Runtime) Option {
	return func(s *Server) {
		s.runtime = runtime
	}
}

func NewServer(options ...Option) (*Server, error) {
	assetsFS, err := newAssetsFS()
	if err != nil {
		return nil, err
	}
	server := &Server{
		assetsFS: assetsFS,
	}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server, nil
}

func (s *Server) initRouter() (*gin.Engine, error) {
	if configlogging.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.DefaultWriter = io.Discard
		gin.DefaultErrorWriter = io.Discard
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.Default()

	// Load the HTML template
	t := template.New("").Funcs(engine.FuncMap)
	template, err := t.ParseFS(content, "html/index.html")
	if err != nil {
		return nil, err
	}
	engine.SetHTMLTemplate(template)

	base_url, err := s.settingService.GetWebPath()
	if err != nil {
		return nil, err
	}

	webDomain, err := s.settingService.GetWebDomain()
	if err != nil {
		return nil, err
	}

	if webDomain != "" {
		engine.Use(domainmiddleware.Validator(webDomain))
	}
	engine.Use(securitymiddleware.AdminForBase(base_url, api.RequestIsHTTPS))
	budgetRegistry := requestbudget.NewRegistry(base_url)
	budgetRegistry.SetPressureGate(func(policy requestbudget.Policy) requestbudget.PressureDecision {
		decision := pressureService.Shared().Admission(policy.PressureClass)
		return requestbudget.PressureDecision{Allowed: decision.Allowed, Reason: decision.ReasonCode, RetryAfter: decision.RetryAfter}
	})
	engine.Use(requestbudget.Middleware(budgetRegistry, s.recordRequestBudgetRejection))

	cookieKeys, err := s.settingService.GetCookieKeys()
	if err != nil {
		return nil, err
	}

	engine.Use(gzip.Gzip(gzip.DefaultCompression))
	assetsBasePath := base_url + "assets/"

	store, err := NewSQLiteSessionStore(dbsqlite.DB(), cookieKeys...)
	if err != nil {
		return nil, err
	}
	engine.Use(sessions.Sessions("s-ui", store))

	engine.Use(func(c *gin.Context) {
		uri := c.Request.RequestURI
		if strings.HasPrefix(uri, assetsBasePath) {
			// Hashed assets are immutable: file name changes whenever
			// content changes.
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
	})

	// Serve the assets folder. We use a custom handler instead of
	// engine.StaticFS so that a missing file responds with 404 directly
	// instead of falling through to NoRoute -> index.html, which made the
	// browser receive HTML for a JS module request after an upgrade and
	// fail with "Failed to load module script: Expected a JavaScript-or-
	// Wasm module script but the server responded with a MIME type of
	// text/html". This was the root cause of the broken Clients tab in
	// upgraded panels: the cached index.html still referenced an old
	// chunk hash that no longer existed in the embedded FS.
	assetsHandler := serveAssetsFS(s.assetsFS)
	engine.GET(assetsBasePath+"*filepath", assetsHandler)
	engine.HEAD(assetsBasePath+"*filepath", assetsHandler)

	group_apiv2 := engine.Group(base_url + "apiv2")
	apiv2 := api.NewAPIv2Handler(group_apiv2, api.WithRuntime(s.runtime))

	group_api := engine.Group(base_url + "api")
	api.NewAPIHandler(group_api, apiv2, api.WithRuntime(s.runtime))
	budgetRegistry.DeclareGinRoutes(engine.Routes())

	// Serve index.html as the entry point
	// Handle all other routes by serving index.html
	engine.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == strings.TrimSuffix(base_url, "/") {
			c.Redirect(http.StatusTemporaryRedirect, base_url)
			return
		}
		if !strings.HasPrefix(c.Request.URL.Path, base_url) {
			if publicsurface.Serve(c, publicsurface.Context{AdminBasePath: base_url}) {
				return
			}
			publicsurface.Handled404(c)
			return
		}
		if c.Request.URL.Path != base_url+"login" && !api.IsLogin(c) {
			c.Redirect(http.StatusTemporaryRedirect, base_url+"login")
			return
		}
		if c.Request.URL.Path == base_url+"login" && api.IsLogin(c) {
			c.Redirect(http.StatusTemporaryRedirect, base_url)
			return
		}
		// index.html must not be cached: it embeds hashed asset URLs
		// and an upgrade rewrites those hashes. A cached index.html
		// after an upgrade would point at chunks that were removed
		// from the embed FS, which is exactly the failure mode that
		// broke the Clients tab in 1.5.x ("Failed to fetch dynamically
		// imported module"). The hashed assets themselves stay
		// immutable; only this entry document needs revalidation.
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.HTML(http.StatusOK, "index.html", gin.H{"BASE_URL": base_url})
	})

	return engine, nil
}

func (s *Server) recordRequestBudgetRejection(c *gin.Context, policy requestbudget.Policy, reason string) {
	peerIP := c.Request.RemoteAddr
	if host, _, err := net.SplitHostPort(peerIP); err == nil {
		peerIP = host
	}
	_ = (&service.AuditService{Runtime: s.runtime}).Record(service.AuditEvent{
		Actor:     "system",
		Event:     "request_budget_rejected",
		Resource:  policy.Route,
		Severity:  service.AuditSeverityWarn,
		IP:        peerIP,
		UserAgent: c.Request.UserAgent(),
		Details: map[string]any{
			"reason":           reason,
			"method":           policy.Method,
			"bodyClass":        policy.BodyClass,
			"concurrencyClass": policy.ConcurrencyClass,
			"auditPolicy":      policy.AuditPolicy,
		},
	})
}

func (s *Server) Start() (err error) {
	//This is an anonymous function, no function name
	defer func() {
		if err != nil {
			_ = s.Stop()
		}
	}()

	engine, err := s.initRouter()
	if err != nil {
		return err
	}

	certFile, err := s.settingService.GetCertFile()
	if err != nil {
		return err
	}
	keyFile, err := s.settingService.GetKeyFile()
	if err != nil {
		return err
	}
	listen, err := s.settingService.GetListen()
	if err != nil {
		return err
	}
	port, err := s.settingService.GetPort()
	if err != nil {
		return err
	}
	webDomain, err := s.settingService.GetWebDomain()
	if err != nil {
		return err
	}
	listenAddr := net.JoinHostPort(listen, strconv.Itoa(port))
	listenResult, err := bind.ListenWithFallbackResult(listenAddr, listen, strconv.Itoa(port))
	if err != nil {
		return err
	}
	listener := listenResult.Listener
	if listenResult.Fallback {
		_ = (&service.AuditService{}).RecordListenFallback("web", listenResult.RequestedAddr, listenResult.FallbackAddr, listenResult.BindError)
	}
	if certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			_ = listener.Close()
			return err
		}
		c := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		listener = autohttps.NewAutoHttpsListener(listener, webDomain)
		listener = tls.NewListener(listener, c)
	}

	if certFile != "" || keyFile != "" {
		logger.Info("web server run https on", listener.Addr())
	} else {
		logger.Info("web server run http on", listener.Addr())
	}
	s.listener = listener

	s.httpServer = &http.Server{
		Handler:           engine,
		MaxHeaderBytes:    requestbudget.MaxHeaderBytes,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Expose the raw connection so long-running import handlers
		// can lift the 30s Read/Write timeouts. The gzip middleware wraps
		// c.Writer such that http.NewResponseController can no longer reach the
		// connection, so the deadline must be set on the conn directly.
		ConnContext: httpconn.SaveContext,
	}
	s.running.Store(true)

	go func() {
		defer s.running.Store(false)
		if serveErr := s.httpServer.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Warning("web server stopped unexpectedly:", serveErr)
		}
	}()
	if s.unregisterHealth == nil {
		unregister, registerErr := componenthealth.Register(panelHealthChecker{server: s})
		if registerErr != nil {
			return registerErr
		}
		s.unregisterHealth = unregister
	}

	return nil
}

func (s *Server) Stop() error {
	s.running.Store(false)
	if s.unregisterHealth != nil {
		s.unregisterHealth()
		s.unregisterHealth = nil
	}
	var err error
	if s.httpServer != nil {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
		err = s.httpServer.Shutdown(shutdownCtx)
		cancelShutdown()
		if err != nil {
			if s.listener != nil {
				_ = s.listener.Close()
			}
			return err
		}
	} else if s.listener != nil {
		err = s.listener.Close()
		if err != nil {
			return err
		}
	}
	s.httpServer = nil
	s.listener = nil
	return nil
}
