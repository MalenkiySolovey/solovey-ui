package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
)

// ClientProvision is the in-process request to create one enabled Panel client.
type ClientProvision struct {
	Name        string          `json:"name"`
	Config      json.RawMessage `json:"config"`
	Inbounds    []uint          `json:"inbounds"`
	Volume      int64           `json:"volume"`
	Expiry      int64           `json:"expiry"`
	Description string          `json:"desc"`
}

// ProvisionClient uses the canonical save transaction and keeps its generation
// admitted through the ID readback. It cannot edit existing clients.
func (s *ConfigService) ProvisionClient(ctx context.Context, client ClientProvision, actor, hostname string) (uint, error) {
	_, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	data, err := json.Marshal(struct {
		ClientProvision
		Enable bool `json:"enable"`
	}{ClientProvision: client, Enable: true})
	if err != nil {
		return 0, err
	}
	if _, err := s.Save(singboxapply.ObjectClients.String(), "new", data, "", actor, hostname); err != nil {
		return 0, fmt.Errorf("save client: %w", err)
	}
	var created model.Client
	if err := configDatabase().Select("id").Where("name = ?", client.Name).First(&created).Error; err != nil {
		return 0, fmt.Errorf("look up created client: %w", err)
	}
	return created.Id, nil
}
