package entityclients

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/backend/users"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

type credentialCapabilities struct{}

func (credentialCapabilities) Available(kind string) bool {
	return entitycapabilities.Resolve("inbounds", kind).Available
}
func (credentialCapabilities) SnellUserKeyMaxBytes() int {
	return registry.SnellContract("inbounds").UserKeyMaxBytes
}
func credentialManager() users.CredentialManager {
	return users.CredentialManager{Capabilities: credentialCapabilities{}, Warning: logger.Warning}
}
func prepareManagedCredentials(tx *gorm.DB, c *model.Client, edit bool) error {
	return credentialManager().PrepareManagedCredentials(tx, c, edit)
}
func PrepareAssignedCredentials(tx *gorm.DB, c *model.Client) error {
	return credentialManager().PrepareAssignedCredentials(tx, c)
}
func PrepareSnellCredential(tx *gorm.DB, c *model.Client) error {
	return credentialManager().PrepareSnellCredential(tx, c)
}
func validateSnellUserKeys(tx *gorm.DB, c []*model.Client) error {
	return credentialManager().ValidateSnellUserKeys(tx, c)
}
func DeliveryCopy(tx *gorm.DB, c *model.Client, inbounds []*model.Inbound) (*model.Client, error) {
	return credentialManager().DeliveryCopy(tx, c, inbounds)
}
