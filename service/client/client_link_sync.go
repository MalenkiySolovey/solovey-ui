package client

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"

	"gorm.io/gorm"
)

func (s *Service) UpdateClientsOnInboundAdd(tx *gorm.DB, initIds string, inboundId uint, hostname string) error {
	return entityclients.UpdateClientsOnInboundAdd(tx, initIds, inboundId, hostname)
}

func (s *Service) UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	return entityclients.UpdateClientsOnInboundDelete(tx, id, tag)
}

func (s *Service) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	return entityclients.UpdateLinksByInboundChange(tx, inbounds, hostname, oldTag)
}
