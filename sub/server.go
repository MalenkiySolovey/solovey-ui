package sub

import (
	"context"
	"errors"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Server struct {
	runtime          *subserver.RuntimeServer
	unregisterHealth func()

	service.SettingService
}

func NewServer() *Server {
	s := &Server{}
	s.runtime = subserver.NewRuntimeServer(
		&s.SettingService,
		func(g *gin.RouterGroup, rateLimit gin.HandlerFunc) {
			newSubHandler(g, rateLimit)
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
	if err := s.runtime.Start(); err != nil {
		return err
	}
	if s.unregisterHealth == nil {
		unregister, registerErr := componenthealth.Register(subscriptionHealthChecker{server: s})
		if registerErr != nil {
			return errors.Join(registerErr, s.runtime.Stop())
		}
		s.unregisterHealth = unregister
	}
	return nil
}

func (s *Server) Stop() error {
	if s.unregisterHealth != nil {
		s.unregisterHealth()
		s.unregisterHealth = nil
	}
	return s.runtime.Stop()
}

type subscriptionHealthChecker struct{ server *Server }

func (subscriptionHealthChecker) ResourceID() string { return "core:subscription:default" }

func (c subscriptionHealthChecker) Check(ctx context.Context) componenthealth.Result {
	if err := ctx.Err(); err != nil {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, FactCode: "health_check_timeout"}
	}
	if c.server == nil || c.server.runtime == nil || !c.server.runtime.ListenerReady() {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "subscription_listener", FactCode: "subscription_listener_unavailable"}
	}
	return componenthealth.Result{Status: componenthealth.StatusOK, Check: "subscription_listener", FactCode: "listener_ready"}
}
