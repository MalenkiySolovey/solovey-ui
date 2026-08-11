package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
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
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/web"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func main() {
	log.SetOutput(os.Stdout)
	fmt.Println("e2e panel-server: init logger")
	logger.Init(logger.LevelWarning)

	fmt.Println("e2e panel-server: init database")
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		log.Fatal(err)
	}
	fmt.Println("e2e panel-server: install registered components")
	if err := installRegisteredComponentsForE2E(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("e2e panel-server: load settings")
	settingService := &service.SettingService{}
	if _, err := settingService.GetAllSetting(); err != nil {
		log.Fatal(err)
	}
	if webPath := os.Getenv("SUI_E2E_WEB_PATH"); webPath != "" {
		if err := settingService.SetComponentSettingString("webPath", webPath); err != nil {
			log.Fatal(err)
		}
	}
	if _, err := settingService.GetAllSetting(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("e2e panel-server: init api-only web server")
	runtime := service.NewRuntime(nil)
	service.SetDefaultRuntime(runtime)
	baseURL, err := settingService.GetWebPath()
	if err != nil {
		log.Fatal(err)
	}
	cookieKeys, err := settingService.GetCookieKeys()
	if err != nil {
		log.Fatal(err)
	}
	store, err := web.NewSQLiteSessionStore(dbsqlite.DB(), cookieKeys...)
	if err != nil {
		log.Fatal(err)
	}
	gin.SetMode(gin.ReleaseMode)
	components := componentsupervisor.New(componenthost.Deps{
		API: componenthost.APIDeps{
			Runtime: runtime,
		},
	})
	service.RegisterComponentMigrator(components.Migrate)
	service.RegisterComponentSettingsReconciler(components.Reconcile)
	service.RegisterComponentDataDropper(components.DropData)
	if err := components.Migrate(context.Background()); err != nil {
		log.Fatal(err)
	}
	if err := components.Start(context.Background()); err != nil {
		log.Fatal(err)
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

	port, err := settingService.GetPort()
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		fmt.Printf("e2e panel-server: listen error: %#v\n", err)
		os.Exit(1)
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
