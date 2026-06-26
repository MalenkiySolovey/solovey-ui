package runtime

import (
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type ClientExpiryJob struct {
	service.ClientService
	service.InboundService
}

func NewClientExpiryJob() *ClientExpiryJob {
	return new(ClientExpiryJob)
}

func (s *ClientExpiryJob) Run() {
	inboundIds, err := s.ClientService.DepleteClients()
	if err != nil {
		logger.Warning("Disable depleted users failed: ", err)
		return
	}
	if len(inboundIds) > 0 {
		err := s.InboundService.RestartCurrentInbounds(inboundIds)
		if err != nil {
			logger.Error("unable to restart inbounds: ", err)
		}
	}
}
