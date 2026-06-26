// Package config owns configuration mutation and core-control HTTP handlers.
package config

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	Runtime           *service.Runtime
	RestartScheduler  service.RestartScheduler
	ConfigService     service.ConfigService
	SettingService    service.SettingService
	UserService       service.UserService
	ClientService     service.ClientService
	TlsService        service.TlsService
	InboundService    service.InboundService
	OutboundService   service.OutboundService
	EndpointService   service.EndpointService
	ServicesService   service.ServicesService
	TelegramService   service.TelegramService
	StatsService      service.StatsService
	ServerService     service.ServerService
	RequireScope      func(*gin.Context, string, ...string) bool
	Actor             func(*gin.Context) string
	Hostname          func(*gin.Context) string
	JSONObj           func(*gin.Context, interface{}, error)
	JSONMsg           func(*gin.Context, string, error)
	Audit             func(*gin.Context, string, string, string, string, map[string]any)
	ReloadPartialData func(*gin.Context, []string) error
	ValidateTarget    func(context.Context, string) error
	RemoteIP          func(*gin.Context) string
}

// Deps contains the host capabilities required by configuration routes.
type Deps struct {
	Runtime          *service.Runtime
	RestartScheduler service.RestartScheduler
	ConfigService    service.ConfigService
	SettingService   service.SettingService
	UserService      service.UserService
	ClientService    service.ClientService
	TlsService       service.TlsService
	InboundService   service.InboundService
	OutboundService  service.OutboundService
	EndpointService  service.EndpointService
	ServicesService  service.ServicesService
	TelegramService  service.TelegramService
	StatsService     service.StatsService
	ServerService    service.ServerService
	RequireScope     func(*gin.Context, string, ...string) bool
	Actor            func(*gin.Context) string
	Hostname         func(*gin.Context) string
	JSONObj          func(*gin.Context, interface{}, error)
	JSONMsg          func(*gin.Context, string, error)
	Audit            func(*gin.Context, string, string, string, string, map[string]any)
	ValidateTarget   func(context.Context, string) error
	RemoteIP         func(*gin.Context) string
	LoginUser        func(*gin.Context) string
}

func NewHandler(deps Deps) *Handler {
	h := &Handler{
		Runtime:          deps.Runtime,
		RestartScheduler: deps.RestartScheduler,
		ConfigService:    deps.ConfigService,
		SettingService:   deps.SettingService,
		UserService:      deps.UserService,
		ClientService:    deps.ClientService,
		TlsService:       deps.TlsService,
		InboundService:   deps.InboundService,
		OutboundService:  deps.OutboundService,
		EndpointService:  deps.EndpointService,
		ServicesService:  deps.ServicesService,
		TelegramService:  deps.TelegramService,
		StatsService:     deps.StatsService,
		ServerService:    deps.ServerService,
		RequireScope:     deps.RequireScope,
		Actor:            deps.Actor,
		Hostname:         deps.Hostname,
		JSONObj:          deps.JSONObj,
		JSONMsg:          deps.JSONMsg,
		Audit:            deps.Audit,
		ValidateTarget:   deps.ValidateTarget,
		RemoteIP:         deps.RemoteIP,
	}
	h.ReloadPartialData = h.LoadPartialData
	return h
}

// RegisterRoutes mounts configuration mutation and panel-data endpoints.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := NewHandler(deps)
	g.POST("/save", func(c *gin.Context) { h.Save(c, deps.LoginUser(c)) })
	g.POST("/reorder", func(c *gin.Context) { h.Reorder(c, deps.LoginUser(c)) })
	g.POST("/restartApp", h.RestartApp)
	g.POST("/restartSb", h.RestartSb)
	g.POST("/linkConvert", h.LinkConvert)
	g.POST("/subConvert", h.SubConvert)
	g.POST("/checkOutbounds", h.CheckOutbounds)
	g.POST("/rotateSubSecret", h.RotateSubSecret)
	g.GET("/singbox-config", h.GetSingboxConfig)
	g.GET("/checkOutbound", h.GetCheckOutbound)

	g.GET("/load", h.LoadData)
	for _, action := range []string{"inbounds", "outbounds", "endpoints", "services", "tls", "clients", "config"} {
		action := action
		g.GET("/"+action, func(c *gin.Context) {
			if err := h.LoadPartialData(c, []string{action}); err != nil {
				h.JSONMsg(c, action, err)
			}
		})
	}
	g.GET("/settings", h.GetSettings)
	g.GET("/settings/schema", h.GetSettingsSchema)
	g.GET("/changes", h.CheckChanges)
}

type Envelope struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}
