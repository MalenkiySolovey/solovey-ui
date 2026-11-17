package users

import (
	shared "github.com/MalenkiySolovey/solovey-host/subscriptions/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

// CredentialCapabilities provides the existing provider availability and Snell contract.
type CredentialCapabilities interface {
	Available(string) bool
	SnellUserKeyMaxBytes() int
}

// CredentialManager owns user credential persistence and authorized delivery.
type CredentialManager struct {
	Capabilities CredentialCapabilities
	Warning      func(...any)
}

func (s CredentialManager) available(kind string) bool {
	return s.Capabilities != nil && s.Capabilities.Available(kind)
}
func (s CredentialManager) snellKeyMaxBytes() int {
	if s.Capabilities == nil {
		return 0
	}
	return s.Capabilities.SnellUserKeyMaxBytes()
}
func (s CredentialManager) canDeliver(kind, format string) bool {
	fact, known := shared.Resolve(kind)
	if !known || !s.available(kind) {
		return false
	}
	switch format {
	case "json":
		return fact.JSON
	case "uri":
		return fact.URI
	default:
		return false
	}
}
func (s CredentialManager) PrepareManagedCredentials(tx *gorm.DB, c *model.Client, editing bool) error {
	return s.prepareManagedCredentials(tx, c, editing)
}
func (s CredentialManager) ValidateSnellUserKeys(tx *gorm.DB, c []*model.Client) error {
	return s.validateSnellUserKeys(tx, c)
}
