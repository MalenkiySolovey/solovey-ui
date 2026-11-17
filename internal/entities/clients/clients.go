package entityclients

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/backend/users"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"gorm.io/gorm"
)

type SaveAction = users.SaveAction

const (
	ActionNew      = users.ActionNew
	ActionEdit     = users.ActionEdit
	ActionAddBulk  = users.ActionAddBulk
	ActionEditBulk = users.ActionEditBulk
	ActionDelBulk  = users.ActionDelBulk
	ActionDel      = users.ActionDel
)

type SaveRequest = users.SaveRequest
type Link = users.Link
type protocolProjection struct{}

func (protocolProjection) ProjectClient(id uint, raw json.RawMessage) (json.RawMessage, error) {
	return credentials.ProjectClient(id, raw)
}
func (protocolProjection) ValidateClientConfig(raw json.RawMessage) error {
	return credentials.ValidateClientConfig(raw)
}
func (protocolProjection) PrepareSnellCredential(tx *gorm.DB, c *model.Client) error {
	return PrepareSnellCredential(tx, c)
}
func (protocolProjection) ValidateSnellUserKeys(tx *gorm.DB, c []*model.Client) error {
	return validateSnellUserKeys(tx, c)
}
func (protocolProjection) PrepareManagedCredentials(tx *gorm.DB, c *model.Client, edit bool) error {
	return prepareManagedCredentials(tx, c, edit)
}
func (protocolProjection) UpdateLinksWithFixedInbounds(tx *gorm.DB, c []*model.Client, host string) error {
	return UpdateLinksWithFixedInbounds(tx, c, host)
}
func (protocolProjection) PreviewWithLocalLinks(tx *gorm.DB, c *[]model.Client, host string) error {
	return PreviewWithLocalLinks(tx, c, host)
}
func userStore() users.Store                              { return users.Store{Protocols: protocolProjection{}} }
func Get(db *gorm.DB, id string) (*[]model.Client, error) { return userStore().Get(db, id) }
func GetWithLocalLinks(db *gorm.DB, id, host string) (*[]model.Client, error) {
	return userStore().GetWithLocalLinks(db, id, host)
}
func GetByID(db *gorm.DB, id string) (*[]model.Client, error) { return userStore().GetByID(db, id) }
func GetAll(db *gorm.DB) (*[]model.Client, error)             { return users.GetAll(db) }
func ValidateStored(db *gorm.DB) error                        { return users.ValidateStored(db) }
func Save(req SaveRequest) ([]uint, error)                    { return userStore().Save(req) }
func SaveImportedCandidate(req SaveRequest) ([]uint, error) {
	return userStore().SaveImportedCandidate(req)
}
func ParseAction(action string) (SaveAction, bool) { return users.ParseAction(action) }
func SupportedActionStrings() []string             { return users.SupportedActionStrings() }
func PrepareSubSecret(tx *gorm.DB, c *model.Client, preserve bool) error {
	return users.PrepareSubSecret(tx, c, preserve)
}
func EnsureSubSecret(tx *gorm.DB, c *model.Client) error   { return users.EnsureSubSecret(tx, c) }
func RotateSubSecret(tx *gorm.DB, id uint) (string, error) { return users.RotateSubSecret(tx, id) }
