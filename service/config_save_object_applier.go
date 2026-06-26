package service

import (
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"gorm.io/gorm"
)

type configServiceCoreObjectApplier struct {
	service *ConfigService
}

func (s *ConfigService) configCoreObjectApplier() singboxapply.ObjectApplier {
	if s.coreObjectApplier != nil {
		return s.coreObjectApplier
	}
	return configServiceCoreObjectApplier{service: s}
}

func (a configServiceCoreObjectApplier) RemoveOutbounds(tags []string) error {
	return a.service.OutboundService.RemoveOutboundsFromCore(tags)
}

func (a configServiceCoreObjectApplier) RemoveEndpoints(tags []string) error {
	return a.service.EndpointService.RemoveEndpointsFromCore(tags)
}

func (a configServiceCoreObjectApplier) RemoveInbounds(tags []string) error {
	return a.service.InboundService.RemoveInboundsFromCore(tags)
}

func (a configServiceCoreObjectApplier) RemoveServices(tags []string) error {
	return a.service.ServicesService.RemoveServicesFromCore(tags)
}

func (a configServiceCoreObjectApplier) RestartOutbounds(tx *gorm.DB, ids []uint) error {
	return a.service.OutboundService.RestartOutbounds(tx, ids)
}

func (a configServiceCoreObjectApplier) RestartEndpoints(tx *gorm.DB, ids []uint) error {
	return a.service.EndpointService.RestartEndpoints(tx, ids)
}

func (a configServiceCoreObjectApplier) RestartInbounds(tx *gorm.DB, ids []uint) error {
	return a.service.InboundService.RestartInbounds(tx, ids)
}

func (a configServiceCoreObjectApplier) RestartServices(tx *gorm.DB, ids []uint) error {
	return a.service.ServicesService.RestartServices(tx, ids)
}
