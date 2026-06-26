package ipmonitor

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func loadHistoryRows(clientName string, limit int) ([]model.ClientIP, error) {
	rows := make([]model.ClientIP, 0)
	db := dbsqlite.DB()
	if db == nil {
		return rows, nil
	}
	err := db.Model(model.ClientIP{}).Where("client_name = ?", clientName).
		Order("last_seen desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func clearHistory(clientName string) error {
	db := dbsqlite.DB()
	if db == nil {
		return nil
	}
	if err := db.Where("client_name = ?", clientName).Delete(&model.ClientIP{}).Error; err != nil {
		return err
	}
	return db.Model(model.Client{}).Where("name = ?", clientName).Update("last_ip_count", 0).Error
}

func PruneOlderThan(before int64) (int64, error) {
	db := dbsqlite.DB()
	if db == nil {
		return 0, nil
	}
	result := db.Where("last_seen < ?", before).Delete(&model.ClientIP{})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		InvalidateAllCache()
	}
	return result.RowsAffected, nil
}

func getInstallSalt() ([]byte, error) {
	ipHashSalt.Lock()
	defer ipHashSalt.Unlock()
	if len(ipHashSalt.value) > 0 {
		return append([]byte(nil), ipHashSalt.value...), nil
	}
	db := dbsqlite.DB()
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	var setting model.Setting
	err := db.Model(model.Setting{}).Where("key = ?", "installSalt").First(&setting).Error
	if dbsqlite.IsNotFound(err) {
		setting = model.Setting{Key: "installSalt", Value: common.Random(32)}
		err = db.Create(&setting).Error
	}
	if err != nil {
		return nil, err
	}
	ipHashSalt.value = []byte(setting.Value)
	return append([]byte(nil), ipHashSalt.value...), nil
}

func getIPShowRaw(now time.Time) (bool, error) {
	ipPrivacySettings.Lock()
	defer ipPrivacySettings.Unlock()
	if now.Before(ipPrivacySettings.expiresAt) {
		return ipPrivacySettings.showRaw, nil
	}
	db := dbsqlite.DB()
	if db == nil {
		ipPrivacySettings.showRaw = false
		ipPrivacySettings.expiresAt = now.Add(allowCacheTTL)
		return false, nil
	}
	var setting model.Setting
	err := db.Model(model.Setting{}).Where("key = ?", "ipShowRaw").First(&setting).Error
	if dbsqlite.IsNotFound(err) {
		ipPrivacySettings.showRaw = false
		ipPrivacySettings.expiresAt = now.Add(allowCacheTTL)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	showRaw, err := strconv.ParseBool(setting.Value)
	if err != nil {
		return false, err
	}
	ipPrivacySettings.showRaw = showRaw
	ipPrivacySettings.expiresAt = now.Add(allowCacheTTL)
	return showRaw, nil
}

func loadCacheEntry(clientName string, now time.Time) (allowCacheEntry, bool) {
	db := dbsqlite.DB()
	if db == nil {
		return allowCacheEntry{}, false
	}
	var client model.Client
	if err := db.Model(model.Client{}).Select("enable, limit_ip, ip_limit_mode").Where("name = ?", clientName).First(&client).Error; err != nil {
		if !dbsqlite.IsNotFound(err) {
			logLoadCacheError("client", err)
		}
		return allowCacheEntry{}, false
	}
	if !client.Enable {
		return allowCacheEntry{}, false
	}
	entry := allowCacheEntry{
		limit: client.LimitIP, mode: client.IPLimitMode,
		ips: map[string]struct{}{}, expiresAt: now.Add(allowCacheTTL),
	}
	rows := make([]model.ClientIP, 0)
	if err := db.Model(model.ClientIP{}).Select("ip, ip_hash").Where("client_name = ?", clientName).Find(&rows).Error; err != nil {
		logLoadCacheError("client_ips", err)
		return allowCacheEntry{}, false
	}
	for _, row := range rows {
		ipHash := row.IPHash
		if ipHash == "" {
			ipHash = hashLegacyIPValue(row.IP)
		}
		if ipHash != "" {
			entry.ips[ipHash] = struct{}{}
		}
	}
	return entry, true
}

type activeEnforceCacheRow struct {
	ClientName  string
	LimitIP     int
	IPLimitMode string
	IP          sql.NullString
	IPHash      sql.NullString
}

func loadActiveEnforceEntries(db *gorm.DB, now time.Time) (map[string]allowCacheEntry, error) {
	rows := make([]activeEnforceCacheRow, 0)
	err := db.Raw(`
		SELECT clients.name AS client_name, clients.limit_ip, clients.ip_limit_mode,
			client_ips.ip, client_ips.ip_hash
		FROM clients
		LEFT JOIN client_ips ON client_ips.client_name = clients.name
		WHERE clients.enable = true AND clients.ip_limit_mode = ? AND clients.limit_ip > 0
		ORDER BY clients.name
	`, ModeEnforce).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make(map[string]allowCacheEntry)
	for _, row := range rows {
		entry, ok := entries[row.ClientName]
		if !ok {
			entry = allowCacheEntry{limit: row.LimitIP, mode: row.IPLimitMode, ips: map[string]struct{}{}, expiresAt: now.Add(allowCacheTTL)}
		}
		ipHash := ""
		if row.IPHash.Valid {
			ipHash = row.IPHash.String
		}
		if ipHash == "" && row.IP.Valid {
			ipHash = hashLegacyIPValue(row.IP.String)
		}
		if ipHash != "" {
			entry.ips[ipHash] = struct{}{}
		}
		entries[row.ClientName] = entry
	}
	return entries, nil
}

func loadWarmUpEntries(now time.Time) (map[string]allowCacheEntry, error) {
	db := dbsqlite.DB()
	if db == nil {
		return map[string]allowCacheEntry{}, nil
	}
	if _, err := getInstallSalt(); err != nil {
		return nil, err
	}
	return loadActiveEnforceEntries(db, now)
}

func Flush() error {
	db := dbsqlite.DB()
	if db == nil {
		return nil
	}
	snapshot := takePendingSnapshot()
	if len(snapshot) == 0 {
		return nil
	}
	tx := db.Begin()
	defer func() {
		if recovered := recover(); recovered != nil {
			tx.Rollback()
			panic(recovered)
		}
	}()
	if err := flushSnapshot(tx, snapshot); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

func FlushTo(tx *gorm.DB) error {
	snapshot := takePendingSnapshot()
	if len(snapshot) == 0 {
		return nil
	}
	return flushSnapshot(tx, snapshot)
}

func takePendingSnapshot() map[string]map[string]pendingIP {
	pending.Lock()
	defer pending.Unlock()
	snapshot := pending.byClient
	pending.byClient = map[string]map[string]pendingIP{}
	return snapshot
}

func flushSnapshot(tx *gorm.DB, snapshot map[string]map[string]pendingIP) error {
	rows := make([]model.ClientIP, 0)
	lastSeenByClient := make(map[string]int64, len(snapshot))
	for clientName, ips := range snapshot {
		for ipHash, item := range ips {
			if item.lastSeen > lastSeenByClient[clientName] {
				lastSeenByClient[clientName] = item.lastSeen
			}
			rows = append(rows, model.ClientIP{
				ClientName: clientName, IPHash: ipHash, IPDisplay: item.display,
				FirstSeen: item.lastSeen, LastSeen: item.lastSeen,
			})
			cacheAddIP(clientName, ipHash)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	batch := dbsqlite.BatchSize(tx, &model.ClientIP{})
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "client_name"}, {Name: "ip_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen", "ip_display"}),
	}).CreateInBatches(&rows, batch).Error; err != nil {
		return err
	}
	for clientName, lastSeen := range lastSeenByClient {
		if err := tx.Model(model.Client{}).Where("name = ?", clientName).Updates(map[string]interface{}{
			"last_online":   lastSeen,
			"last_ip_count": gorm.Expr("(SELECT COUNT(*) FROM client_ips WHERE client_name = ?)", clientName),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
