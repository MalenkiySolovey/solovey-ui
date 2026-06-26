package failover

import (
	servicefailover "github.com/MalenkiySolovey/solovey-ui/service/failover"
	"github.com/gin-gonic/gin"
)

type Deps struct {
	Status  func() ([]servicefailover.StatusEntry, error)
	JSONObj func(*gin.Context, any, error)
}

type Handler struct {
	deps Deps
}

func RegisterRoutes(group *gin.RouterGroup, deps Deps) {
	handler := Handler{deps: deps}
	group.GET("/failover-status", handler.status)
}

func (h Handler) status(context *gin.Context) {
	status, err := h.deps.Status()
	h.deps.JSONObj(context, status, err)
}
