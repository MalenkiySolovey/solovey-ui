package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/api"
	_ "github.com/MalenkiySolovey/solovey-ui/app"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	componentsupervisor "github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	cronscheduler "github.com/MalenkiySolovey/solovey-ui/cronjob/scheduler"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/web"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func main() {
	fmt.Println("e2e panel-server: init logger")
	logger.Init(logger.LevelWarning)

	fmt.Println("e2e panel-server: init database")
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		fatal("database initialization", err)
	}
	fmt.Println("e2e panel-server: install registered components")
	if err := installRegisteredComponentsForE2E(); err != nil {
		fatal("component installation", err)
	}
	fmt.Println("e2e panel-server: load settings")
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		fatal("initial settings load", err)
	}
	if webPath := os.Getenv("SUI_E2E_WEB_PATH"); webPath != "" {
		if err := settingService.SetWebPath(webPath); err != nil {
			fatal("web path configuration", err)
		}
	}
	if _, err := settingService.GetAllSetting(); err != nil {
		fatal("settings reload", err)
	}

	fmt.Println("e2e panel-server: init api-only web server")
	runtime := service.NewRuntime(nil)
	service.SetDefaultRuntime(runtime)
	cronScheduler := cronscheduler.New()
	if err := cronScheduler.Start(time.UTC, 0); err != nil {
		fatal("scheduler startup", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cronScheduler.Stop(ctx)
	}()
	baseURL, err := settingService.GetWebPath()
	if err != nil {
		fatal("web path lookup", err)
	}
	cookieKeys, err := settingService.GetCookieKeys()
	if err != nil {
		fatal("session key setup", err)
	}
	store, err := web.NewSQLiteSessionStore(dbsqlite.DB(), cookieKeys...)
	if err != nil {
		fatal("session store setup", err)
	}
	gin.SetMode(gin.ReleaseMode)
	components := componentsupervisor.New(componenthost.Deps{
		API: componenthost.APIDeps{
			Runtime: runtime,
		},
		Scheduler: cronScheduler,
	})
	service.RegisterComponentMigrator(components.Migrate)
	service.RegisterComponentSettingsReconciler(components.Reconcile)
	service.RegisterComponentDataDropper(components.DropData)
	if err := components.Migrate(context.Background()); err != nil {
		fatal("component migration", err)
	}
	if err := components.Start(context.Background()); err != nil {
		fatal("component startup", err)
	}
	defer func() {
		_ = components.Stop(context.Background())
	}()
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(securitymiddleware.Admin(api.RequestIsHTTPS))
	engine.Use(sessions.Sessions("s-ui", store))
	groupAPIV2 := engine.Group(baseURL + "apiv2")
	apiv2 := api.NewAPIv2Handler(groupAPIV2, api.WithRuntime(runtime))
	groupAPI := engine.Group(baseURL + "api")
	api.NewAPIHandler(groupAPI, apiv2, api.WithRuntime(runtime))
	engine.GET(baseURL, func(c *gin.Context) {
		c.String(http.StatusOK, "e2e panel")
	})
	engine.GET(baseURL+"login", func(c *gin.Context) {
		c.String(http.StatusOK, "e2e panel")
	})
	engine.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == strings.TrimSuffix(baseURL, "/") {
			c.Redirect(http.StatusTemporaryRedirect, baseURL)
			return
		}
		if !strings.HasPrefix(c.Request.URL.Path, baseURL) {
			if publicsurface.Serve(c, publicsurface.Context{AdminBasePath: baseURL}) {
				return
			}
			publicsurface.Handled404(c)
			return
		}
		c.String(http.StatusNotFound, "e2e not found")
	})

	listenAddress := strings.TrimSpace(os.Getenv("SUI_E2E_BACKEND_LISTEN"))
	if listenAddress == "" {
		port, err := settingService.GetPort()
		if err != nil {
			fatal("web port lookup", err)
		}
		listenAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		fatal("backend listen", err)
	}
	defer listener.Close()
	if err := writeBackendAddress(listener.Addr().String()); err != nil {
		fatal("backend address publication", err)
	}
	server := &http.Server{
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Warning("e2e api-only server stopped:", err)
		}
	}()
	fmt.Println("e2e panel-server: ready")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = server.Shutdown(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func fatal(stage string, err error) {
	fmt.Fprintf(os.Stderr, "e2e panel-server: %s failed: %v\n", stage, err)
	os.Exit(1)
}

func writeBackendAddress(address string) error {
	path := strings.TrimSpace(os.Getenv("SUI_E2E_BACKEND_ADDRESS_FILE"))
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(address+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func installRegisteredComponentsForE2E() error {
	registered := registry.Components()
	components := make([]installstate.InstalledComponent, 0, len(registered))
	for _, component := range registered {
		components = append(components, installstate.InstalledComponent{
			ID:        component.Manifest.ID,
			Delivery:  component.Manifest.Delivery,
			Installed: true,
		})
	}
	if err := installstate.Store(installstate.DefaultPath(), installstate.Metadata{
		Version:    1,
		Profile:    "e2e-full",
		Binary:     "full",
		Components: components,
	}); err != nil {
		return err
	}
	state.InvalidateActiveCache()
	return nil
}
