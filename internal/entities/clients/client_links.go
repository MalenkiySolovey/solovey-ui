package entityclients

import (
	"bytes"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
	"strings"
)

func LinkString(link Link, key string) string {
	value, _ := link[key].(string)
	return value
}
func DecodeLinks(clientID uint, raw json.RawMessage, operation string) ([]Link, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []Link{}, true
	}
	var links []Link
	if err := json.Unmarshal(raw, &links); err != nil {
		logger.Warningf("%s skipped client %d with invalid links: %v", operation, clientID, err)
		return nil, false
	}
	return links, true
}
func BuildLinksForInbounds(config json.RawMessage, inbounds []model.Inbound, hostname string) []Link {
	links := []Link{}
	for i := range inbounds {
		for _, uri := range suburi.Generate(config, &inbounds[i], hostname) {
			links = append(links, Link{
				"remark": inbounds[i].Tag,
				"type":   "local",
				"uri":    uri,
			})
		}
	}
	return links
}
func RebuildLinks(clientID uint, config, rawLinks json.RawMessage, inbounds []model.Inbound, hostname string, keep func(link Link) bool, operation string) (json.RawMessage, bool, error) {
	clientLinks, ok := DecodeLinks(clientID, rawLinks, operation)
	if !ok {
		return nil, false, nil
	}
	newClientLinks := BuildLinksForInbounds(config, inbounds, hostname)
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
func UpdateLinksWithFixedInbounds(tx *gorm.DB, clients []*model.Client, hostname string) error {
	inboundCache := map[string][]model.Inbound{}
	for index, client := range clients {
		var inboundIDs []uint
		if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
			return err
		}
		cacheKey := string(client.Inbounds)
		inbounds, cached := inboundCache[cacheKey]
		if !cached {
			if len(inboundIDs) > 0 {
				if err := tx.Model(model.Inbound{}).Preload("Tls").
					Where("id in ? and type in ?", inboundIDs, suburi.SupportedInboundTypes).
					Find(&inbounds).Error; err != nil {
					return err
				}
			}
			inboundCache[cacheKey] = inbounds
		}
		links, ok, err := RebuildLinks(client.Id, client.Config, client.Links, inbounds, hostname, func(link Link) bool {
			return LinkString(link, "type") != "local"
		}, "fixed inbound link update")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		clients[index].Links = links
	}
	return nil
}
func PreviewWithLocalLinks(db *gorm.DB, clients *[]model.Client, hostname string) error {
	if clients == nil || len(*clients) == 0 {
		return nil
	}
	inboundCache := map[string][]model.Inbound{}
	for index := range *clients {
		client := &(*clients)[index]
		var inboundIDs []uint
		if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
			return err
		}
		cacheKey := string(client.Inbounds)
		inbounds, cached := inboundCache[cacheKey]
		if !cached {
			if len(inboundIDs) > 0 {
				if err := db.Model(model.Inbound{}).Preload("Tls").
					Where("id in ? and type in ?", inboundIDs, suburi.SupportedInboundTypes).
					Find(&inbounds).Error; err != nil {
					return err
				}
			}
			inboundCache[cacheKey] = inbounds
		}
		links, ok, err := RebuildLinks(client.Id, client.Config, client.Links, inbounds, hostname, func(link Link) bool {
			return LinkString(link, "type") != "local"
		}, "client local link preview")
		if err != nil {
			return err
		}
		if ok {
			client.Links = links
		}
	}
	return nil
}
func UpdateClientsOnInboundAdd(tx *gorm.DB, initIDs string, inboundID uint, hostname string) error {
	clientIDs := strings.Split(initIDs, ",")
	var clients []model.Client
	err := tx.Model(model.Client{}).Where("id in ?", clientIDs).Find(&clients).Error
	if err != nil {
		return err
	}
	var inbound model.Inbound
	err = tx.Model(model.Inbound{}).Preload("Tls").Where("id = ?", inboundID).Find(&inbound).Error
	if err != nil {
		return err
	}
	for _, client := range clients {
		var ok bool
		client.Inbounds, ok, err = AppendInbound(client.Id, client.Inbounds, inboundID, "inbound add")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		links, decoded, lerr := RebuildLinks(client.Id, client.Config, client.Links, []model.Inbound{inbound}, hostname, func(link Link) bool {
			return LinkString(link, "remark") != inbound.Tag
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
func UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	clients, err := ByInbound(tx, id)
	if err != nil {
		return err
	}
	for _, client := range clients {
		var ok bool
		client.Inbounds, ok, err = RemoveInbound(client.Id, client.Inbounds, id, "inbound delete")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		clientLinks, ok := DecodeLinks(client.Id, client.Links, "inbound delete")
		if !ok {
			continue
		}
		var newClientLinks []Link
		for _, clientLink := range clientLinks {
			if LinkString(clientLink, "remark") != tag {
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
func UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	for _, inbound := range *inbounds {
		clients, err := ByInbound(tx, inbound.Id)
		if err != nil {
			return err
		}
		for _, client := range clients {
			links, decoded, lerr := RebuildLinks(client.Id, client.Config, client.Links, []model.Inbound{inbound}, hostname, func(link Link) bool {
				return LinkString(link, "type") != "local" || (LinkString(link, "remark") != inbound.Tag && LinkString(link, "remark") != oldTag)
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
func UpdateResetFields(tx *gorm.DB, clientID uint, values map[string]interface{}) error {
	return tx.Model(model.Client{}).Where("id = ?", clientID).Updates(values).Error
}
