package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"

	"gorm.io/gorm"
)

const clientHasInboundCondition = "? IN (SELECT json_each.value FROM json_each(clients.inbounds))"

type clientInboundNameRow struct {
	InboundID uint
	Name      string
}

func decodeClientInbounds(clientID uint, raw json.RawMessage, operation string) ([]uint, bool) {
	var inbounds []uint
	if err := json.Unmarshal(raw, &inbounds); err != nil {
		logger.Warningf("%s skipped client %d with invalid inbounds: %v", operation, clientID, err)
		return nil, false
	}
	return inbounds, true
}

func appendClientInbound(clientID uint, raw json.RawMessage, inboundID uint, operation string) (json.RawMessage, bool, error) {
	inboundIDs, ok := decodeClientInbounds(clientID, raw, operation)
	if !ok {
		return nil, false, nil
	}
	inboundIDs = append(inboundIDs, inboundID)
	marshaled, err := json.MarshalIndent(inboundIDs, "", "  ")
	if err != nil {
		return nil, true, err
	}
	return marshaled, true, nil
}

func removeClientInbound(clientID uint, raw json.RawMessage, inboundID uint, operation string) (json.RawMessage, bool, error) {
	inboundIDs, ok := decodeClientInbounds(clientID, raw, operation)
	if !ok {
		return nil, false, nil
	}
	var nextInboundIDs []uint
	for _, existing := range inboundIDs {
		if existing != inboundID {
			nextInboundIDs = append(nextInboundIDs, existing)
		}
	}
	marshaled, err := json.MarshalIndent(nextInboundIDs, "", "  ")
	if err != nil {
		return nil, true, err
	}
	return marshaled, true, nil
}

func clientIDsByInbound(tx *gorm.DB, inboundID uint) ([]uint, error) {
	var clientIDs []uint
	err := tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ? ORDER BY clients.sort_order, clients.id", inboundID).Scan(&clientIDs).Error
	return clientIDs, err
}

func clientsByInbound(tx *gorm.DB, inboundID uint) ([]model.Client, error) {
	clientIDs, err := clientIDsByInbound(tx, inboundID)
	if err != nil || len(clientIDs) == 0 {
		return nil, err
	}
	var clients []model.Client
	err = tx.Model(model.Client{}).Where("id IN ?", clientIDs).Order(sortOrderClause).Find(&clients).Error
	return clients, err
}

func clientNamesByInboundIDs(db *gorm.DB, inboundIDs []uint) (map[uint][]string, error) {
	usersByInbound := make(map[uint][]string, len(inboundIDs))
	if len(inboundIDs) == 0 {
		return usersByInbound, nil
	}
	for _, id := range inboundIDs {
		usersByInbound[id] = []string{}
	}

	var rows []clientInboundNameRow
	err := db.Raw(`
		SELECT je.value AS inbound_id, clients.name
		FROM clients, json_each(clients.inbounds) AS je
		WHERE je.value IN ?
		ORDER BY clients.sort_order, clients.id, je.key
	`, inboundIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		usersByInbound[row.InboundID] = append(usersByInbound[row.InboundID], row.Name)
	}
	return usersByInbound, nil
}
