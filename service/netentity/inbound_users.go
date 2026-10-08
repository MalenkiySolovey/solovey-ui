package netentity

import (
	"encoding/json"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	"gorm.io/gorm"
)

func (s *InboundService) hasUser(kind string) bool {
	return (entityinbounds.StoredUsers{}).HasUser(kind)
}
func (s *InboundService) AddUsers(db *gorm.DB, data []byte, id uint, kind string) ([]byte, error) {
	return (entityinbounds.StoredUsers{}).AddUsers(db, data, id, kind)
}
func (s *InboundService) addUsers(db *gorm.DB, data []byte, id uint, kind string) ([]byte, error) {
	return s.AddUsers(db, data, id, kind)
}
func (s *InboundService) fetchUsers(db *gorm.DB, kind string, inbound map[string]interface{}, id uint) ([]json.RawMessage, error) {
	return (entityinbounds.StoredUsers{}).FetchUsers(db, kind, inbound, id)
}
