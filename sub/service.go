package sub

import (
	"encoding/json"
	"errors"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	canonical "github.com/MalenkiySolovey/solovey-backend/subscriptions"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	clientfacts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"strings"
)

type SubService struct{ service.SettingService }

func (s *SubService) GetSubs(id string) (*string, []string, error) {
	return publisherFor(&s.SettingService).GetSubs(id)
}
func (s *SubService) getClientBySubId(id string) (*model.Client, error) {
	return publisherFor(&s.SettingService).LookupClient(id)
}
func resolveClientLinks(raw json.RawMessage, mode sublocal.LinkMode, info string) []string {
	return sublocal.ResolveClientLinks(raw, mode, info)
}
func buildClientHeaders(c *model.Client, cfg subserver.DisplaySettings) []string {
	return canonical.BuildClientHeaders(c, cfg)
}

// privateLinkDelivery materializes managed links only after subscription
// authorization. Canonical Client.Links and encrypted credentials stay intact.
func privateLinkDelivery(client *model.Client) (*model.Client, error) {
	db := dbsqlite.DB()
	if raw := strings.TrimSpace(string(client.Inbounds)); raw == "" || raw == "null" {
		return client, nil
	}
	var ids []uint
	if err := json.Unmarshal(client.Inbounds, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return client, nil
	}
	var assigned []*model.Inbound
	if err := db.Preload("Tls").Where("id in ?", ids).Find(&assigned).Error; err != nil {
		return nil, err
	}
	inbounds := make([]*model.Inbound, 0, len(assigned))
	for _, inbound := range assigned {
		fact, known := clientfacts.Resolve(inbound.Type)
		if !known || fact.SecretField == "" || !clientfacts.CanDeliver(inbound.Type, "uri") {
			continue
		}
		copy, err := entityinbounds.OutboundDeliveryCopy(db, inbound)
		if err != nil {
			return nil, err
		}
		inbounds = append(inbounds, copy)
	}
	if len(inbounds) == 0 {
		return client, nil
	}
	copy, err := entityclients.DeliveryCopy(db, client, inbounds)
	if err != nil {
		return nil, err
	}
	defer common.WipeBytes(copy.Config)
	links, ok := entityclients.DecodeLinks(client.Id, client.Links, "private subscription delivery")
	if !ok {
		return nil, errors.New("CLIENT_LINKS_INVALID")
	}
	for _, inbound := range inbounds {
		generated, err := suburi.Generate(copy.Config, inbound, "")
		if err != nil {
			return nil, err
		}
		for _, link := range generated {
			links = append(links, entityclients.Link{"type": "local", "remark": inbound.Tag, "uri": link})
		}
	}
	copy.Config = nil // Never return a borrowed credential buffer after wiping it.
	links, err = entityclients.NameLinks(links)
	if err != nil {
		return nil, err
	}
	copy.Links, err = json.Marshal(links)
	return copy, err
}

func loadDelivery(client *model.Client) (*model.Client, []*model.Inbound, error) {
	db := dbsqlite.DB()
	var err error
	var inboundIDs []uint
	if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
		return nil, nil, err
	}
	var inbounds []*model.Inbound
	if err := db.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIDs).Find(&inbounds).Error; err != nil {
		return nil, nil, err
	}
	available := inbounds[:0]
	for _, inbound := range inbounds {
		if !entitycapabilities.ResolveResource("inbounds", inbound.Type, inbound.Options).Available {
			continue
		}
		copy, err := entityinbounds.OutboundDeliveryCopy(db, inbound)
		if err != nil {
			return nil, nil, err
		}
		available = append(available, copy)
	}
	inbounds = available
	client, err = entityclients.DeliveryCopy(db, client, inbounds)
	if err != nil {
		return nil, nil, err
	}
	return client, inbounds, nil
}
