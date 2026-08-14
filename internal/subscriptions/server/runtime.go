package server

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	domainmiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/domain"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/network/autohttps"
	"github.com/MalenkiySolovey/solovey-ui/network/bind"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

type Settings interface {
	GetSubPath() (string, error)
	GetSubDomain() (string, error)
	GetSubJsonPath() (string, error)
	GetSubClashPath() (string, error)
	GetSubXrayPath() (string, error)
	GetSubCertFile() (string, error)
	GetSubKeyFile() (string, error)
	GetSubListen() (string, error)
	GetSubPort() (int, error)
}

type BaseRoutes func(*gin.RouterGroup, gin.HandlerFunc)
type FormatHandlersFactory func() FormatHandlers

type RuntimeServer struct {
	httpServer *http.Server
	listener   net.Listener
	running    atomic.Bool

	settings      Settings
	baseRoutes    BaseRoutes
	formatFactory FormatHandlersFactory
	rateLimiter   *RateLimiter
}

func NewRuntimeServer(settings Settings, baseRoutes BaseRoutes, formatFactory FormatHandlersFactory) *RuntimeServer {
	return &RuntimeServer{
		settings:      settings,
		baseRoutes:    baseRoutes,
		formatFactory: formatFactory,
		rateLimiter:   NewRateLimiter(),
	}
}

func (s *RuntimeServer) InitRouter() (*gin.Engine, error) {
	if configlogging.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.DefaultWriter = io.Discard
		gin.DefaultErrorWriter = io.Discard
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.Default()

	subPath, err := s.settings.GetSubPath()
	if err != nil {
		return nil, err
	}
	subDomain, err := s.settings.GetSubDomain()
	if err != nil {
		return nil, err
	}

	if subDomain != "" {
		engine.Use(domainmiddleware.Validator(subDomain))
	}
	engine.Use(securitymiddleware.Subscriptions())
	engine.Use(gzip.Gzip(gzip.DefaultCompression))

	registeredFormats := map[string]string{}
	if err := RememberPath(registeredFormats, subPath, "link"); err != nil {
		return nil, err
	}
	if err := RememberPath(registeredFormats, JoinPath(subPath, "json"), "json"); err != nil {
		return nil, err
	}
	if err := RememberPath(registeredFormats, JoinPath(subPath, "clash"), "clash"); err != nil {
		return nil, err
	}
	if err := RememberPath(registeredFormats, JoinPath(subPath, "xray"), "xray"); err != nil {
		return nil, err
	}

	if s.baseRoutes != nil {
		s.baseRoutes(engine.Group(subPath), s.rateLimiter.Middleware())
	}
	if subPath != "/" {
		if err := s.registerFormatRoute(engine, registeredFormats, "/json/", "json"); err != nil {
			return nil, err
		}
		if err := s.registerFormatRoute(engine, registeredFormats, "/clash/", "clash"); err != nil {
			return nil, err
		}
		if err := s.registerFormatRoute(engine, registeredFormats, "/xray/", "xray"); err != nil {
			return nil, err
		}
	}
	if err := s.registerCustomFormatRoutes(engine, registeredFormats); err != nil {
		return nil, err
	}

	return engine, nil
}

func (s *RuntimeServer) registerCustomFormatRoutes(engine *gin.Engine, registered map[string]string) error {
	jsonPath, err := s.settings.GetSubJsonPath()
	if err != nil {
		return err
	}
	clashPath, err := s.settings.GetSubClashPath()
	if err != nil {
		return err
	}
	xrayPath, err := s.settings.GetSubXrayPath()
	if err != nil {
		return err
	}
	if err := s.registerFormatRoute(engine, registered, jsonPath, "json"); err != nil {
		return err
	}
	if err := s.registerFormatRoute(engine, registered, clashPath, "clash"); err != nil {
		return err
	}
	return s.registerFormatRoute(engine, registered, xrayPath, "xray")
}

func (s *RuntimeServer) registerFormatRoute(engine *gin.Engine, registered map[string]string, path string, format string) error {
	var handlers FormatHandlers
	if s.formatFactory != nil {
		handlers = s.formatFactory()
	}
	return RegisterFormatRoute(engine, registered, path, format, handlers, s.rateLimiter)
}

func (s *RuntimeServer) Start() (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(err, s.Stop())
		}
	}()

	engine, err := s.InitRouter()
	if err != nil {
		return err
	}

	certFile, err := s.settings.GetSubCertFile()
	if err != nil {
		return err
	}
	keyFile, err := s.settings.GetSubKeyFile()
	if err != nil {
		return err
	}
	listen, err := s.settings.GetSubListen()
	if err != nil {
		return err
	}
	port, err := s.settings.GetSubPort()
	if err != nil {
		return err
	}
	subDomain, err := s.settings.GetSubDomain()
	if err != nil {
		return err
	}

	listenAddr := net.JoinHostPort(listen, strconv.Itoa(port))
	listenResult, err := bind.ListenWithFallbackResult(listenAddr, listen, strconv.Itoa(port))
	if err != nil {
		return err
	}
	listener := listenResult.Listener
	s.listener = listener
	if listenResult.Fallback {
		if hook := currentHooks().ListenFallbackAudit; hook != nil {
			hook("sub", listenResult.RequestedAddr, listenResult.FallbackAddr, listenResult.BindError)
		}
	}

	if certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return err
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		listener = autohttps.NewAutoHttpsListener(listener, subDomain)
		listener = tls.NewListener(listener, tlsConfig)
	}

	if certFile != "" || keyFile != "" {
		logger.Info("Sub server run https on", listener.Addr())
	} else {
		logger.Info("Sub server run http on", listener.Addr())
	}
	s.listener = listener

	s.httpServer = &http.Server{
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.running.Store(true)

	go func() {
		defer s.running.Store(false)
		if serveErr := s.httpServer.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Warning("Sub server stopped unexpectedly:", serveErr)
		}
	}()

	return nil
}

func (s *RuntimeServer) Stop() error {
	s.running.Store(false)
	var err error
	if s.httpServer != nil {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
		err = s.httpServer.Shutdown(shutdownCtx)
		cancelShutdown()
		if err != nil {
			if s.listener != nil {
				err = errors.Join(err, s.listener.Close())
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

// ListenerReady exposes only lifecycle state; it never derives or probes a
// subscription URL, so callers cannot turn it into a secret-path health probe.
func (s *RuntimeServer) ListenerReady() bool {
	return s != nil && s.listener != nil && s.httpServer != nil && s.running.Load()
}
