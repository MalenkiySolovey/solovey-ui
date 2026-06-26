package sub

import (
	"context"

	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Server struct {
	runtime *subserver.RuntimeServer

	service.SettingService
}

func NewServer() *Server {
	s := &Server{}
	s.runtime = subserver.NewRuntimeServer(
		&s.SettingService,
		func(g *gin.RouterGroup) {
			NewSubHandler(g)
		},
		func() subserver.FormatHandlers {
			handler := &SubHandler{}
			return subserver.FormatHandlers{
				JSON:    handler.json,
				Clash:   handler.clash,
				Xray:    handler.xray,
				Headers: handler.subHeaders,
			}
		},
	)
	return s
}

func (s *Server) initRouter() (*gin.Engine, error) {
	return s.runtime.InitRouter()
}

func (s *Server) Start() error {
	return s.runtime.Start()
}

func (s *Server) Stop() error {
	return s.runtime.Stop()
}

func (s *Server) GetCtx() context.Context {
	return s.runtime.Context()
}
