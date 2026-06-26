package box

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/tracker"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

func (s *Box) Uptime() uint32 {
	return uint32(time.Since(s.createdAt).Seconds())
}

func (s *Box) Network() adapter.NetworkManager {
	return s.network
}

func (s *Box) Router() adapter.Router {
	return s.router
}

func (s *Box) Inbound() adapter.InboundManager {
	return s.inbound
}

func (s *Box) Outbound() adapter.OutboundManager {
	return s.outbound
}

func (s *Box) Service() adapter.ServiceManager {
	return s.service
}

func (s *Box) Endpoint() adapter.EndpointManager {
	return s.endpoint
}

func (s *Box) LogFactory() log.Factory {
	return s.logFactory
}

func (s *Box) StatsTracker() *tracker.StatsTracker {
	return s.statsTracker
}

func (s *Box) ConnTracker() *tracker.ConnTracker {
	return s.connTracker
}
