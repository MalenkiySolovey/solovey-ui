package sub

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	canonical "github.com/MalenkiySolovey/solovey-backend/subscriptions"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type SubHandler struct {
	service.SettingService
	SubService
	JSONService
	ClashService
	XrayService
}
type handlerPublisher struct{ handler *SubHandler }

func (p handlerPublisher) GetSubs(id string) (*string, []string, error) {
	return p.handler.SubService.GetSubs(id)
}
func (p handlerPublisher) GetJSON(id string) (*string, []string, error) {
	return p.handler.JSONService.GetJSON(id)
}
func (p handlerPublisher) GetClash(id string) (*string, []string, error) {
	return p.handler.ClashService.GetClash(id)
}
func (p handlerPublisher) GetXray(id string) (*string, []string, error) {
	return p.handler.XrayService.GetXray(id)
}
func (p handlerPublisher) LookupClient(id string) (*model.Client, error) {
	return p.handler.SubService.getClientBySubId(id)
}

const maxSubscriptionHeaderBytes = 512

func (s *SubHandler) backend() *canonical.Handler {
	return &canonical.Handler{Settings: &s.SettingService, Publisher: handlerPublisher{s}, Error: logger.Error}
}
func newSubHandler(g *gin.RouterGroup, rate gin.HandlerFunc) { (&SubHandler{}).initRouter(g, rate) }
func (s *SubHandler) initRouter(g *gin.RouterGroup, rate gin.HandlerFunc) {
	s.backend().InitRouter(g, rate)
}
func (s *SubHandler) subs(c *gin.Context)       { s.backend().Subs(c) }
func (s *SubHandler) json(c *gin.Context)       { s.backend().Json(c) }
func (s *SubHandler) clash(c *gin.Context)      { s.backend().Clash(c) }
func (s *SubHandler) xray(c *gin.Context)       { s.backend().Xray(c) }
func (s *SubHandler) subHeaders(c *gin.Context) { s.backend().SubHeaders(c) }
func (s *SubHandler) formatHeaders(format string) gin.HandlerFunc {
	return s.backend().FormatHeaders(format)
}
func (s *SubHandler) addHeaders(c *gin.Context, h []string) { s.backend().AddHeaders(c, h) }
func safeSubscriptionHeaders(h []string) []string           { return canonical.SafeSubscriptionHeaders(h) }
