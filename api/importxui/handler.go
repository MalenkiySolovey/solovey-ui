// Package importxui owns the HTTP route surface for 3x-ui migration.
package importxui

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	AuditService service.AuditService
	RequireScope func(*gin.Context, string, ...string) bool
	Audit        func(*gin.Context, string, string, string, string, map[string]any)
	Actor        func(*gin.Context) string
	RemoteIP     func(*gin.Context) string
	Hostname     func(*gin.Context) string
	JSONObj      func(*gin.Context, interface{}, error)
	JSONMsg      func(*gin.Context, string, error)
}

// Deps contains the host capabilities required by 3x-ui import routes.
type Deps struct {
	AuditService service.AuditService
	RequireScope func(*gin.Context, string, ...string) bool
	Audit        func(*gin.Context, string, string, string, string, map[string]any)
	Actor        func(*gin.Context) string
	RemoteIP     func(*gin.Context) string
	Hostname     func(*gin.Context) string
	JSONObj      func(*gin.Context, interface{}, error)
	JSONMsg      func(*gin.Context, string, error)
}

type Upload struct {
	Dir      string
	Path     string
	SHA256   string
	Fields   map[string]string
	PlanPath string
	PlanSize int64
}

type Envelope struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

type RouteSpec struct {
	Method string
	Path   string
}

var RouteSpecs = []RouteSpec{
	{Method: http.MethodPost, Path: "/import-xui"},
	{Method: http.MethodPost, Path: "/import-xui/plan"},
	{Method: http.MethodPost, Path: "/import-xui/apply"},
	{Method: http.MethodPost, Path: "/import-xui/rollback"},
	{Method: http.MethodGet, Path: "/import-xui/reports"},
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		AuditService: deps.AuditService,
		RequireScope: deps.RequireScope,
		Audit:        deps.Audit,
		Actor:        deps.Actor,
		RemoteIP:     deps.RemoteIP,
		Hostname:     deps.Hostname,
		JSONObj:      deps.JSONObj,
		JSONMsg:      deps.JSONMsg,
	}
}

// RegisterRoutes mounts 3x-ui import routes using RouteSpecs as the route registry.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := NewHandler(deps)
	handlers := []gin.HandlerFunc{
		h.ImportXui,
		h.ImportXuiPlan,
		h.ImportXuiApply,
		h.ImportXuiRollback,
		h.ImportXuiReports,
	}
	for i, spec := range RouteSpecs {
		g.Handle(spec.Method, spec.Path, handlers[i])
	}
}
