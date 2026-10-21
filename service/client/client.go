// Package client owns client persistence, membership, links, and lifecycle operations.
package client

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const clientTrafficOverLimitCondition = "volume > 0 AND (up > volume OR down > volume OR up > volume - down)"

type Service struct {
	touchLastUpdate func(int64)
}

func New(setLastUpdate func(int64)) Service {
	return Service{touchLastUpdate: setLastUpdate}
}

func (s *Service) setLastUpdate(value int64) {
	if s != nil && s.touchLastUpdate != nil {
		s.touchLastUpdate(value)
	}
}

func clientDatabase() *gorm.DB {
	return dbsqlite.DB()
}

func (s *Service) Get(id string) (*[]model.Client, error) {
	return entityclients.Get(clientDatabase(), id)
}

func (s *Service) GetWithLocalLinks(id string, hostname string) (*[]model.Client, error) {
	return entityclients.GetWithLocalLinks(clientDatabase(), id, hostname)
}

func (s *Service) GetAll() (*[]model.Client, error) {
	return entityclients.GetAll(clientDatabase())
}

func (s *Service) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) ([]uint, error) {
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

func (s *Service) DepleteClients() (inboundIds []uint, err error) {
	var clients []model.Client
	var changes []model.Changes
	var resetChanged bool

	dt := time.Now().Unix()
	db := clientDatabase()
	if db == nil {
		return nil, errors.New("client database is not initialized")
	}

	tx := db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if err == nil {
			err = tx.Commit().Error
			if err != nil {
				return
			}
			if resetChanged || len(changes) > 0 {
				s.setLastUpdate(dt)
			}
			if err1 := db.Exec("PRAGMA wal_checkpoint(FULL)").Error; err1 != nil {
				logger.Error("Error checkpointing WAL: ", err1.Error())
			}
		} else {
			tx.Rollback()
		}
	}()

	// Reset clients
	inboundIds, resetChanged, err = s.resetClients(tx, dt)
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
		err = tx.Model(model.Client{}).Where("enable = true AND (("+clientTrafficOverLimitCondition+") OR (expiry > 0 AND expiry < ?))", dt).Updates(map[string]interface{}{
			"enable":                false,
			"access_disable_reason": gorm.Expr("CASE WHEN expiry > 0 AND expiry < ? THEN ? ELSE ? END", dt, model.ClientDisabledExpired, model.ClientDisabledQuota),
		}).Error
		if err != nil {
			return nil, err
		}
		err = dbsqlite.CreateInBatches(tx.Model(model.Changes{}), &changes)
		if err != nil {
			return nil, err
		}
	}

	return inboundIds, nil
}

// ResetClients stages reset effects in the caller's transaction. The caller
// owns commit and freshness publication; DepleteClients publishes after commit.
func (s *Service) ResetClients(tx *gorm.DB, dt int64) ([]uint, error) {
	ids, _, err := s.resetClients(tx, dt)
	return ids, err
}

func (s *Service) resetClients(tx *gorm.DB, dt int64) ([]uint, bool, error) {
	var err error
	var resetClients []*model.Client
	var changes []model.Changes
	var inboundIds []uint
	// Set delay start without periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = false AND (up > 0 OR down > 0)").Find(&resetClients).Error
	if err != nil {
		return nil, false, err
	}
	for _, client := range resetClients {
		client.Expiry = dt + (clientResetPeriodDays(client.ResetDays) * 86400)
		client.DelayStart = false
		if err := updateClientResetFields(tx, client.Id, map[string]interface{}{
			"expiry":      client.Expiry,
			"delay_start": client.DelayStart,
		}); err != nil {
			return nil, false, err
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
		return nil, false, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (clientResetPeriodDays(client.ResetDays) * 86400)
		client.DelayStart = false
		if err := updateClientResetFields(tx, client.Id, map[string]interface{}{
			"next_reset":  client.NextReset,
			"delay_start": client.DelayStart,
		}); err != nil {
			return nil, false, err
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
		return nil, false, err
	}
	for _, client := range resetClients {
		var clientInboundIds []uint
		if !client.Enable {
			var ok bool
			clientInboundIds, ok = decodeClientInbounds(client.Id, client.Inbounds, "client reset")
			if !ok {
				continue
			}
		}
		// Evaluate current access state and counters in the write itself. A
		// policy update after selection must never be overwritten by this job.
		// The period fence also prevents double accounting or a stale schedule.
		result := tx.Model(model.Client{}).
			Where("id = ? AND auto_reset = true AND delay_start = false AND next_reset = ? AND reset_days = ?", client.Id, client.NextReset, client.ResetDays).
			Updates(map[string]interface{}{
				"next_reset":            dt + (clientResetPeriodDays(client.ResetDays) * 86400),
				"total_up":              gorm.Expr("total_up + up"),
				"total_down":            gorm.Expr("total_down + down"),
				"up":                    0,
				"down":                  0,
				"enable":                gorm.Expr("CASE WHEN enable = false AND access_disable_reason = ? AND (expiry <= 0 OR expiry >= ?) THEN true ELSE enable END", model.ClientDisabledQuota, dt),
				"access_disable_reason": gorm.Expr("CASE WHEN access_disable_reason = ? THEN CASE WHEN expiry > 0 AND expiry < ? THEN ? ELSE '' END ELSE access_disable_reason END", model.ClientDisabledQuota, dt, model.ClientDisabledExpired),
			})
		if result.Error != nil {
			return nil, false, result.Error
		}
		if result.RowsAffected > 0 {
			changes = append(changes, model.Changes{DateTime: dt, Actor: "ResetJob", Key: "clients", Action: "reset", Obj: clientChangeNameJSON(client.Name)})
			inboundIds = common.UnionUintArray(inboundIds, clientInboundIds)
		}
	}

	// Save changes
	if len(changes) > 0 {
		err = dbsqlite.CreateInBatches(tx.Model(model.Changes{}), &changes)
		if err != nil {
			return nil, false, err
		}
	}
	return inboundIds, len(changes) > 0, nil
}

func (s *Service) ResetTraffic(id string) (client model.Client, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return client, common.NewError("client id is required")
	}
	clientID, parseErr := strconv.ParseUint(id, 10, 64)
	if parseErr != nil || clientID == 0 {
		return client, common.NewError("invalid client id")
	}

	db := clientDatabase()
	tx := db.Begin()
	if tx.Error != nil {
		return client, tx.Error
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	}()

	if err = tx.Model(model.Client{}).Where("id = ?", clientID).First(&client).Error; err != nil {
		return client, err
	}
	dt := time.Now().Unix()
	if err = tx.Model(model.Client{}).Where("id = ?", clientID).Updates(map[string]interface{}{
		"total_up":   gorm.Expr("total_up + up"),
		"total_down": gorm.Expr("total_down + down"),
		"up":         0,
		"down":       0,
	}).Error; err != nil {
		return client, err
	}
	if err = tx.Model(model.Changes{}).Create(&model.Changes{
		DateTime: dt,
		Actor:    "ResetTraffic",
		Key:      "clients",
		Action:   "resetTraffic",
		Obj:      clientChangeNameJSON(client.Name),
	}).Error; err != nil {
		return client, err
	}
	if err = tx.Commit().Error; err != nil {
		return client, err
	}
	committed = true
	s.setLastUpdate(dt)

	client.TotalUp += client.Up
	client.TotalDown += client.Down
	client.Up = 0
	client.Down = 0
	return client, nil
}

func clientResetPeriodDays(resetDays int) int64 {
	if resetDays < 1 {
		return 1
	}
	return int64(resetDays)
}

func updateClientResetFields(tx *gorm.DB, clientID uint, values map[string]interface{}) error {
	return entityclients.UpdateResetFields(tx, clientID, values)
}
