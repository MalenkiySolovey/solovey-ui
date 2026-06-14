package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const clientTrafficOverLimitCondition = "volume > 0 AND (up > volume OR down > volume OR up > volume - down)"

type ClientService struct {
	Runtime *Runtime
}

func (s *ClientService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *ClientService) setLastUpdate(value int64) {
	s.runtime().updates().Set(value)
}

func (s *ClientService) Get(id string) (*[]model.Client, error) {
	if id == "" {
		return s.GetAll()
	}
	return s.getById(id)
}

func (s *ClientService) GetWithLocalLinks(id string, hostname string) (*[]model.Client, error) {
	clients, err := s.Get(id)
	if err != nil || id == "" {
		return clients, err
	}
	if err := s.previewClientsWithLocalLinks(clients, hostname); err != nil {
		return nil, err
	}
	return clients, nil
}

func (s *ClientService) getById(id string) (*[]model.Client, error) {
	db := database.GetDB()
	var client []model.Client
	err := db.Model(model.Client{}).Where("id in ?", strings.Split(id, ",")).Order(sortOrderClause).Scan(&client).Error
	if err != nil {
		return nil, err
	}

	return &client, nil
}

func (s *ClientService) GetAll() (*[]model.Client, error) {
	db := database.GetDB()
	var clients []model.Client
	err := db.Model(model.Client{}).
		Select("`id`, `sort_order`, `enable`, `name`, `sub_secret`, `desc`, `group`, `inbounds`, `up`, `down`, `volume`, `expiry`, `limit_ip`, `ip_limit_mode`, `last_online`, `last_ip_count`").
		Order(sortOrderClause).
		Scan(&clients).Error
	if err != nil {
		return nil, err
	}
	return &clients, nil
}

func (s *ClientService) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) ([]uint, error) {
	return s.applyClientSave(clientSaveRequest{
		tx:       tx,
		action:   act,
		data:     data,
		hostname: hostname,
	})
}

// clientChangeNameJSON marshals a client name as a JSON string for the
// Changes.Obj payload. Building it by raw concatenation ("\"" + name + "\"")
// breaks when the name contains a quote, backslash or control character: the
// resulting json.RawMessage is invalid and later fails json.Marshal of the
// whole changes feed (CheckChanges then returns an empty body for all admins).
func clientChangeNameJSON(name string) json.RawMessage {
	b, err := json.Marshal(name)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return b
}

func (s *ClientService) DepleteClients() (inboundIds []uint, err error) {
	var clients []model.Client
	var changes []model.Changes

	dt := time.Now().Unix()
	db := database.GetDB()

	tx := db.Begin()
	defer func() {
		if err == nil {
			err = tx.Commit().Error
			if err != nil {
				return
			}
			if err1 := db.Exec("PRAGMA wal_checkpoint(FULL)").Error; err1 != nil {
				logger.Error("Error checkpointing WAL: ", err1.Error())
			}
		} else {
			tx.Rollback()
		}
	}()

	// Reset clients
	inboundIds, err = s.ResetClients(tx, dt)
	if err != nil {
		return nil, err
	}

	// Deplete clients
	err = tx.Model(model.Client{}).Where("enable = true AND (("+clientTrafficOverLimitCondition+") OR (expiry > 0 AND expiry < ?))", dt).Scan(&clients).Error
	if err != nil {
		return nil, err
	}

	for _, client := range clients {
		logger.Debug("Client ", client.Name, " is going to be disabled")
		userInbounds, ok := decodeClientInbounds(client.Id, client.Inbounds, "client deplete")
		if !ok {
			continue
		}
		// Find changed inbounds
		inboundIds = common.UnionUintArray(inboundIds, userInbounds)
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "DepleteJob",
			Key:      "clients",
			Action:   "disable",
			Obj:      clientChangeNameJSON(client.Name),
		})
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Client{}).Where("enable = true AND (("+clientTrafficOverLimitCondition+") OR (expiry > 0 AND expiry < ?))", dt).Update("enable", false).Error
		if err != nil {
			return nil, err
		}
		err = database.CreateInBatchesSafe(tx.Model(model.Changes{}), &changes)
		if err != nil {
			return nil, err
		}
		s.setLastUpdate(dt)
	}

	return inboundIds, nil
}

func (s *ClientService) ResetClients(tx *gorm.DB, dt int64) ([]uint, error) {
	var err error
	var resetClients []*model.Client
	var changes []model.Changes
	var inboundIds []uint
	// Set delay start without periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = false AND (up > 0 OR down > 0)").Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.Expiry = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		if err := updateClientResetFields(tx, client.Id, map[string]interface{}{
			"expiry":      client.Expiry,
			"delay_start": client.DelayStart,
		}); err != nil {
			return nil, err
		}
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      clientChangeNameJSON(client.Name),
		})
	}

	// Set delay start with periodic reset
	resetClients = nil
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = true AND (up > 0 OR down > 0)").Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		if err := updateClientResetFields(tx, client.Id, map[string]interface{}{
			"next_reset":  client.NextReset,
			"delay_start": client.DelayStart,
		}); err != nil {
			return nil, err
		}
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      clientChangeNameJSON(client.Name),
		})
	}

	// Set periodic reset
	resetClients = nil
	err = tx.Model(model.Client{}).
		Where("delay_start = false AND auto_reset = true AND next_reset < ?", dt).Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		if !client.Enable {
			clientInboundIds, ok := decodeClientInbounds(client.Id, client.Inbounds, "client reset")
			if !ok {
				continue
			}
			inboundIds = common.UnionUintArray(inboundIds, clientInboundIds)
		}
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.TotalUp += client.Up
		client.TotalDown += client.Down
		client.Up = 0
		client.Down = 0
		if !client.Enable {
			client.Enable = true
		}
		if err := updateClientResetFields(tx, client.Id, map[string]interface{}{
			"next_reset": client.NextReset,
			"total_up":   client.TotalUp,
			"total_down": client.TotalDown,
			"up":         client.Up,
			"down":       client.Down,
			"enable":     client.Enable,
		}); err != nil {
			return nil, err
		}
	}

	// Save changes
	if len(changes) > 0 {
		err = database.CreateInBatchesSafe(tx.Model(model.Changes{}), &changes)
		if err != nil {
			return nil, err
		}
		s.setLastUpdate(dt)
	}
	return inboundIds, nil
}

func updateClientResetFields(tx *gorm.DB, clientID uint, values map[string]interface{}) error {
	return tx.Model(model.Client{}).Where("id = ?", clientID).Updates(values).Error
}

func (s *ClientService) findInboundsChanges(tx *gorm.DB, client *model.Client, fillOmitted bool) ([]uint, error) {
	var err error
	var oldClient model.Client
	var oldInboundIds, newInboundIds []uint
	err = tx.Model(model.Client{}).Where("id = ?", client.Id).First(&oldClient).Error
	if err != nil {
		return nil, err
	}
	if fillOmitted {
		client.Links = oldClient.Links
		client.Config = oldClient.Config
	}
	err = json.Unmarshal(oldClient.Inbounds, &oldInboundIds)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(client.Inbounds, &newInboundIds)
	if err != nil {
		return nil, err
	}

	// Check client.Config changes
	if !bytes.Equal(oldClient.Config, client.Config) ||
		oldClient.Name != client.Name ||
		oldClient.Enable != client.Enable {
		return common.UnionUintArray(oldInboundIds, newInboundIds), nil
	}

	// Check client.Inbounds changes
	diffInbounds := common.DiffUintArray(oldInboundIds, newInboundIds)

	return diffInbounds, nil
}
