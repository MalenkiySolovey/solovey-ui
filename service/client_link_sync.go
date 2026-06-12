package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util"

	"gorm.io/gorm"
)

func decodeClientLinks(clientID uint, raw json.RawMessage, operation string) ([]map[string]string, bool) {
	// A migrated (or freshly inserted) client can have a NULL/empty Links
	// column. Treat that as "no links yet" rather than an error, otherwise the
	// link-regeneration paths skip the client and its inbounds never appear in
	// the subscription even after an inbound edit.
	if len(bytes.TrimSpace(raw)) == 0 {
		return []map[string]string{}, true
	}
	var links []map[string]string
	if err := json.Unmarshal(raw, &links); err != nil {
		logger.Warningf("%s skipped client %d with invalid links: %v", operation, clientID, err)
		return nil, false
	}
	return links, true
}

// buildLinksForInbounds generates the "local" link entries for the given
// inbounds. The result is always a non-nil slice so an empty result marshals to
// `[]`, never `null`.
func buildLinksForInbounds(config json.RawMessage, inbounds []model.Inbound, hostname string) []map[string]string {
	links := []map[string]string{}
	for i := range inbounds {
		for _, uri := range util.LinkGenerator(config, &inbounds[i], hostname) {
			links = append(links, map[string]string{
				"remark": inbounds[i].Tag,
				"type":   "local",
				"uri":    uri,
			})
		}
	}
	return links
}

func rebuildClientLinks(clientID uint, config, rawLinks json.RawMessage, inbounds []model.Inbound, hostname string, keep func(link map[string]string) bool, operation string) (json.RawMessage, bool, error) {
	clientLinks, ok := decodeClientLinks(clientID, rawLinks, operation)
	if !ok {
		return nil, false, nil
	}
	newClientLinks := buildLinksForInbounds(config, inbounds, hostname)
	for _, clientLink := range clientLinks {
		if keep(clientLink) {
			newClientLinks = append(newClientLinks, clientLink)
		}
	}
	marshaled, err := json.MarshalIndent(newClientLinks, "", "  ")
	if err != nil {
		return nil, true, err
	}
	return marshaled, true, nil
}

func (s *ClientService) updateLinksWithFixedInbounds(tx *gorm.DB, clients []*model.Client, hostname string) error {
	// Each client may carry a different inbound set (notably act="editbulk", where
	// ClientEditBulk.vue preserves per-client inbounds), so regenerate links from
	// the current client's own Inbounds JSON.
	inboundCache := map[string][]model.Inbound{}
	for index, client := range clients {
		var inboundIds []uint
		if err := json.Unmarshal(client.Inbounds, &inboundIds); err != nil {
			return err
		}
		cacheKey := string(client.Inbounds)
		inbounds, cached := inboundCache[cacheKey]
		if !cached {
			if len(inboundIds) > 0 {
				if err := tx.Model(model.Inbound{}).Preload("Tls").
					Where("id in ? and type in ?", inboundIds, util.InboundTypeWithLink).
					Find(&inbounds).Error; err != nil {
					return err
				}
			}
			inboundCache[cacheKey] = inbounds
		}
		links, ok, err := rebuildClientLinks(client.Id, client.Config, client.Links, inbounds, hostname, func(link map[string]string) bool {
			return link["type"] != "local"
		}, "fixed inbound link update")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		// #nosec G602 -- index is the range index over clients; never out of range.
		clients[index].Links = links
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundAdd(tx *gorm.DB, initIds string, inboundId uint, hostname string) error {
	clientIds := strings.Split(initIds, ",")
	var clients []model.Client
	err := tx.Model(model.Client{}).Where("id in ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	var inbound model.Inbound
	err = tx.Model(model.Inbound{}).Preload("Tls").Where("id = ?", inboundId).Find(&inbound).Error
	if err != nil {
		return err
	}
	for _, client := range clients {
		var ok bool
		client.Inbounds, ok, err = appendClientInbound(client.Id, client.Inbounds, inboundId, "inbound add")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		links, decoded, lerr := rebuildClientLinks(client.Id, client.Config, client.Links, []model.Inbound{inbound}, hostname, func(link map[string]string) bool {
			return link["remark"] != inbound.Tag
		}, "inbound add")
		if lerr != nil {
			return lerr
		}
		if !decoded {
			continue
		}
		client.Links = links
		if err = tx.Save(&client).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	clients, err := clientsByInbound(tx, id)
	if err != nil {
		return err
	}
	for _, client := range clients {
		var ok bool
		client.Inbounds, ok, err = removeClientInbound(client.Id, client.Inbounds, id, "inbound delete")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		clientLinks, ok := decodeClientLinks(client.Id, client.Links, "inbound delete")
		if !ok {
			continue
		}
		var newClientLinks []map[string]string
		for _, clientLink := range clientLinks {
			if clientLink["remark"] != tag {
				newClientLinks = append(newClientLinks, clientLink)
			}
		}
		client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	for _, inbound := range *inbounds {
		clients, err := clientsByInbound(tx, inbound.Id)
		if err != nil {
			return err
		}
		for _, client := range clients {
			links, decoded, lerr := rebuildClientLinks(client.Id, client.Config, client.Links, []model.Inbound{inbound}, hostname, func(link map[string]string) bool {
				return link["type"] != "local" || (link["remark"] != inbound.Tag && link["remark"] != oldTag)
			}, "inbound link update")
			if lerr != nil {
				return lerr
			}
			if !decoded {
				continue
			}
			client.Links = links
			if err = tx.Save(&client).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
