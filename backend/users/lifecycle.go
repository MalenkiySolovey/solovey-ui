// Package users owns Panel VPN user access and durable quota lifecycle commands.
package users

import (
	"encoding/json"
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"time"
)

const clientTrafficOverLimitCondition = "volume > 0 AND (up > volume OR down > volume OR up > volume - down)"

// Lifecycle receives Product freshness and logging sinks. Every durable operation
// uses the existing Panel connection and preserves caller-owned transactions.
type Lifecycle struct {
	TouchLastUpdate func(int64)
	Error           func(...any)
	Debug           func(...any)
	Warningf        func(string, ...any)
}

func (s Lifecycle) touch(stamp int64) {
	if s.TouchLastUpdate != nil {
		s.TouchLastUpdate(stamp)
	}
}
func (s Lifecycle) logError(args ...any) {
	if s.Error != nil {
		s.Error(args...)
	}
}
func (s Lifecycle) debug(args ...any) {
	if s.Debug != nil {
		s.Debug(args...)
	}
}
func (s Lifecycle) decodeInbounds(id uint, raw json.RawMessage, operation string) ([]uint, bool) {
	return DecodeInboundMembership(id, raw, operation, s.Warningf)
}

// DecodeInboundMembership preserves persisted membership rejection and diagnostics.
func DecodeInboundMembership(id uint, raw json.RawMessage, operation string, warningf func(string, ...any)) ([]uint, bool) {
	var ids []uint
	if err := json.Unmarshal(raw, &ids); err != nil {
		if warningf != nil {
			warningf("%s skipped client %d with invalid inbounds: %v", operation, id, err)
		}
		return nil, false
	}
	return ids, true
}

func unionInboundIDs(left, right []uint) []uint {
	values := make(map[uint]struct{}, len(left)+len(right))
	for _, id := range left {
		values[id] = struct{}{}
	}
	for _, id := range right {
		values[id] = struct{}{}
	}
	var result []uint
	for id := range values {
		result = append(result, id)
	}
	return result
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

func (s Lifecycle) DepleteClients() (inboundIds []uint, err error) {
	var clients []model.Client
	var changes []model.Changes
	var resetChanged bool

	dt := time.Now().Unix()
	db := dbsqlite.DB()
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
				s.touch(dt)
			}
			if err1 := db.Exec("PRAGMA wal_checkpoint(FULL)").Error; err1 != nil {
				s.logError("Error checkpointing WAL: ", err1.Error())
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
		s.debug("Client ", client.Name, " is going to be disabled")
		userInbounds, ok := s.decodeInbounds(client.Id, client.Inbounds, "client deplete")
		if !ok {
			continue
		}
		// Find changed inbounds
		inboundIds = unionInboundIDs(inboundIds, userInbounds)
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
func (s Lifecycle) ResetClients(tx *gorm.DB, dt int64) ([]uint, error) {
	ids, _, err := s.resetClients(tx, dt)
	return ids, err
}

func (s Lifecycle) resetClients(tx *gorm.DB, dt int64) ([]uint, bool, error) {
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
			clientInboundIds, ok = s.decodeInbounds(client.Id, client.Inbounds, "client reset")
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
			inboundIds = unionInboundIDs(inboundIds, clientInboundIds)
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

func (s Lifecycle) ResetTraffic(id string) (client model.Client, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return client, errors.New("client id is required")
	}
	clientID, parseErr := strconv.ParseUint(id, 10, 64)
	if parseErr != nil || clientID == 0 {
		return client, errors.New("invalid client id")
	}

	db := dbsqlite.DB()
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
	s.touch(dt)

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
	return tx.Model(model.Client{}).Where("id = ?", clientID).Updates(values).Error
}
