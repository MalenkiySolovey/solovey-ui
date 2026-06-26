// Package auth owns login, administrator, and API-token HTTP handlers.
package auth

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	UserService              service.UserService
	SettingService           service.SettingService
	TelegramService          service.TelegramService
	JSONObj                  func(*gin.Context, interface{}, error)
	JSONMsg                  func(*gin.Context, string, error)
	JSONMsgObj               func(*gin.Context, string, interface{}, error)
	Audit                    func(*gin.Context, string, string, string, string, map[string]any)
	LoginUser                func(*gin.Context) string
	SetLoginUser             func(*gin.Context, string, int, string) error
	ClearSession             func(*gin.Context)
	RemoteIP                 func(*gin.Context) string
	CheckLoginRateLimit      func(string) error
	RecordLoginFailure       func(string)
	ResetLoginFailures       func(string)
	LoginRateLimitUserKey    func(string) string
	LoginUsernameTarpitDelay func(string) time.Duration
}

// Deps contains the host capabilities required by authentication routes.
type Deps struct {
	UserService              service.UserService
	SettingService           service.SettingService
	TelegramService          service.TelegramService
	JSONObj                  func(*gin.Context, interface{}, error)
	JSONMsg                  func(*gin.Context, string, error)
	JSONMsgObj               func(*gin.Context, string, interface{}, error)
	Audit                    func(*gin.Context, string, string, string, string, map[string]any)
	LoginUser                func(*gin.Context) string
	SetLoginUser             func(*gin.Context, string, int, string) error
	ClearSession             func(*gin.Context)
	RemoteIP                 func(*gin.Context) string
	CheckLoginRateLimit      func(string) error
	RecordLoginFailure       func(string)
	ResetLoginFailures       func(string)
	LoginRateLimitUserKey    func(string) string
	LoginUsernameTarpitDelay func(string) time.Duration
	CSRF                     gin.HandlerFunc
	ReloadTokensAfter        func(gin.HandlerFunc) gin.HandlerFunc
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		UserService:              deps.UserService,
		SettingService:           deps.SettingService,
		TelegramService:          deps.TelegramService,
		JSONObj:                  deps.JSONObj,
		JSONMsg:                  deps.JSONMsg,
		JSONMsgObj:               deps.JSONMsgObj,
		Audit:                    deps.Audit,
		LoginUser:                deps.LoginUser,
		SetLoginUser:             deps.SetLoginUser,
		ClearSession:             deps.ClearSession,
		RemoteIP:                 deps.RemoteIP,
		CheckLoginRateLimit:      deps.CheckLoginRateLimit,
		RecordLoginFailure:       deps.RecordLoginFailure,
		ResetLoginFailures:       deps.ResetLoginFailures,
		LoginRateLimitUserKey:    deps.LoginRateLimitUserKey,
		LoginUsernameTarpitDelay: deps.LoginUsernameTarpitDelay,
	}
}

// RegisterRoutes mounts authentication, administrator, and API-token routes.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := NewHandler(deps)
	g.POST("/login", h.Login)
	g.POST("/changePass", h.ChangePass)
	g.POST("/addAdmin", h.AddAdmin)
	g.POST("/deleteAdmin", deps.ReloadTokensAfter(h.DeleteAdmin))
	g.POST("/logoutAllAdmins", h.LogoutAllAdmins)

	g.GET("/csrf", deps.CSRF)
	g.POST("/logout", h.Logout)
	g.GET("/users", h.GetUsers)

	g.POST("/addToken", deps.ReloadTokensAfter(h.AddToken))
	g.POST("/deleteToken", deps.ReloadTokensAfter(h.DeleteToken))
	g.POST("/setTokenEnabled", deps.ReloadTokensAfter(h.SetTokenEnabled))
	g.GET("/tokens", h.GetTokens)
}
