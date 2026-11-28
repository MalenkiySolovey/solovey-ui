//go:build !minimal

package blockcheck

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"gorm.io/gorm"
)

var errHistoryStorage = errors.New("blockcheck_history_unavailable")

const historyPageRuns = 8
const historyPageBytes = 2 << 20

type historyRunRow struct {
	ID         string `gorm:"primaryKey;index:idx_blockcheck_history_finished,priority:2"`
	FinishedAt int64  `gorm:"not null;index:idx_blockcheck_history_finished,priority:1"`
	CheckAll   bool   `gorm:"not null"`
	Payload    []byte `gorm:"not null"`
}

func (historyRunRow) TableName() string { return feature.HistoryRunTable }

type historyGroupRow struct {
	RunID    string `gorm:"primaryKey"`
	GroupID  string `gorm:"primaryKey;index"`
	Position int    `gorm:"not null"`
	Payload  []byte `gorm:"not null"`
}

func (historyGroupRow) TableName() string { return feature.HistoryGroupTable }

type historyFilter struct {
	all     bool
	groupID string
	before  string
}

type historyKey struct {
	ID         string
	FinishedAt int64
}

func (k historyKey) cursor() string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(k.FinishedAt, 10) + ":" + k.ID))
}

func parseHistoryCursor(value string) (historyKey, bool) {
	if len(value) > 128 {
		return historyKey{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	stamp, id, found := strings.Cut(string(decoded), ":")
	finished, numberErr := strconv.ParseInt(stamp, 10, 64)
	return historyKey{ID: id, FinishedAt: finished}, err == nil && found && numberErr == nil && validHistoryID(id)
}

func validHistoryID(value string) bool {
	return value != "" && len(value) <= 64 && strings.Trim(value, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
}

func readHistoryPage(ctx context.Context, db *gorm.DB, now time.Time, filter historyFilter) (feature.History, error) {
	page := feature.History{Version: 1, Runs: []feature.HistoryRun{}, Paged: true}
	if db == nil {
		return page, errHistoryStorage
	}
	before, valid := parseHistoryCursor(filter.before)
	if filter.before != "" && !valid || filter.groupID != "" && (filter.all || !validHistoryID(filter.groupID)) {
		return page, feature.ErrInvalidHistory
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&historyRunRow{}).Where("finished_at != 0 AND finished_at >= ?", now.Add(-feature.MaxHistoryAge).UnixMicro())
		if filter.all {
			query = query.Where("check_all = ?", true)
		}
		if filter.groupID != "" {
			query = query.Where("id IN (?)", tx.Model(&historyGroupRow{}).Select("run_id").Where("group_id = ?", filter.groupID))
		}
		if valid {
			query = query.Where("(finished_at < ? OR (finished_at = ? AND id < ?))", before.FinishedAt, before.FinishedAt, before.ID)
		}
		var keys []historyKey
		if err := query.Select("id", "finished_at").Order("finished_at DESC, id DESC").Limit(historyPageRuns + 1).Find(&keys).Error; err != nil {
			return errHistoryStorage
		}
		remaining := historyPageBytes - 1024 // API envelope, cursor and the run separators
		for i, key := range keys {
			if i == historyPageRuns {
				page.Next = keys[i-1].cursor()
				break
			}
			run, size, err := readHistoryRun(tx, key, filter.groupID, remaining)
			if err != nil {
				return err
			}
			if i != 0 && (size > remaining || run.Hidden != 0) {
				// Give the next run a full page before returning a partial view.
				page.Next = keys[i-1].cursor()
				break
			}
			page.Runs = append(page.Runs, run)
			remaining -= size + 1
		}
		return nil
	})
	return page, err
}

func readHistoryRun(tx *gorm.DB, key historyKey, onlyGroup string, remaining int) (feature.HistoryRun, int, error) {
	var row historyRunRow
	var run feature.HistoryRun
	if err := tx.Where("id = ?", key.ID).Take(&row).Error; err != nil {
		return run, 0, errHistoryStorage
	}
	if json.Unmarshal(row.Payload, &run) != nil || run.ID != row.ID || run.FinishedAt.UnixMicro() != row.FinishedAt {
		return run, 0, feature.ErrInvalidHistory
	}
	run.Groups = []feature.HistoryGroup{}
	query := tx.Model(&historyGroupRow{}).Where("run_id = ?", key.ID)
	if onlyGroup != "" {
		query = query.Where("group_id = ?", onlyGroup)
		run.GroupIDs, run.Omitted = []string{onlyGroup}, 0
	}
	meta, err := json.Marshal(run)
	if err != nil {
		return run, 0, feature.ErrInvalidHistory
	}
	size := len(meta) + 64 // room for the projection's hidden count
	if size > historyPageBytes-1024 {
		// Extremely large suites keep their progress counters; individual group
		// details are still addressable through the group history endpoint.
		run.Hidden, run.GroupIDs = len(run.GroupIDs), nil
		return run, 1024, nil
	}
	var entries []struct {
		GroupID string
		Size    int
	}
	if err := query.Select("group_id, length(payload) AS size").Order("position ASC, group_id ASC").Find(&entries).Error; err != nil {
		return run, 0, errHistoryStorage
	}
	for i, entry := range entries {
		if entry.Size <= 0 || entry.Size > feature.MaxHistoryGroupBytes {
			return run, 0, feature.ErrInvalidHistory
		}
		if size+entry.Size+1 > remaining {
			run.Hidden = len(entries) - i
			break
		}
		var stored historyGroupRow
		if err := tx.Where("run_id = ? AND group_id = ?", key.ID, entry.GroupID).Take(&stored).Error; err != nil {
			return run, 0, errHistoryStorage
		}
		var item feature.HistoryGroup
		if json.Unmarshal(stored.Payload, &item) != nil || item.Report.GroupID != entry.GroupID {
			return run, 0, feature.ErrInvalidHistory
		}
		item.Redact()
		run.Groups = append(run.Groups, item)
		size += entry.Size + 1
	}
	return run, size, nil
}

func writeHistoryGroup(tx *gorm.DB, id string, position int, item feature.HistoryGroup) error {
	if !validHistoryID(id) || !validHistoryID(item.Report.GroupID) {
		return feature.ErrInvalidHistory
	}
	item.Report.Results = slices.Clone(item.Report.Results)
	item.Redact()
	payload, err := json.Marshal(item)
	if err != nil || len(payload) > feature.MaxHistoryGroupBytes {
		return feature.ErrInvalidHistory
	}
	row := historyGroupRow{RunID: id, GroupID: item.Report.GroupID, Position: position, Payload: payload}
	if err := tx.Create(&row).Error; err != nil {
		return errHistoryStorage
	}
	return nil
}

func beginHistory(ctx context.Context, db *gorm.DB, id string, now time.Time) error {
	if db == nil || !validHistoryID(id) {
		return errHistoryStorage
	}
	return writeStoredBlockCheck(ctx, db, errHistoryStorage, func(tx *gorm.DB) error {
		if err := pruneHistory(tx, now); err != nil {
			return err
		}
		return tx.Create(&historyRunRow{ID: id, Payload: []byte("{}")}).Error
	})
}

func writeHistoryRun(tx *gorm.DB, run feature.HistoryRun) error {
	if !validHistoryID(run.ID) || run.StartedAt.IsZero() || run.FinishedAt.Before(run.StartedAt) {
		return feature.ErrInvalidHistory
	}
	for i, item := range run.Groups { // legacy migration and callers with a complete run
		if err := writeHistoryGroup(tx, run.ID, i, item); err != nil {
			return err
		}
	}
	run.Groups, run.Hidden = nil, 0
	payload, err := json.Marshal(run)
	if err != nil {
		return feature.ErrInvalidHistory
	}
	row := historyRunRow{ID: run.ID, FinishedAt: run.FinishedAt.UnixMicro(), CheckAll: run.All, Payload: payload}
	update := tx.Model(&historyRunRow{}).Where("id = ? AND finished_at = 0", row.ID).
		Updates(map[string]any{"finished_at": row.FinishedAt, "check_all": row.CheckAll, "payload": row.Payload})
	if update.Error != nil {
		return errHistoryStorage
	}
	if update.RowsAffected == 1 {
		return nil
	}
	if err := tx.Create(&row).Error; err != nil {
		return errHistoryStorage
	}
	return nil
}

func pruneHistory(tx *gorm.DB, now time.Time) error {
	cutoff := now.Add(-feature.MaxHistoryAge).UnixMicro()
	// A zero timestamp identifies interrupted runs. The owner admits only one
	// foreground writer, so this is safe before starting the next run.
	expired := tx.Model(&historyRunRow{}).Select("id").Where("finished_at = 0 OR finished_at < ?", cutoff)
	if err := tx.Where("run_id IN (?)", expired).Delete(&historyGroupRow{}).Error; err != nil {
		return errHistoryStorage
	}
	if err := tx.Where("finished_at = 0 OR finished_at < ?", cutoff).Delete(&historyRunRow{}).Error; err != nil {
		return errHistoryStorage
	}
	return nil
}

func retainHistory(ctx context.Context, db *gorm.DB, run feature.HistoryRun) error {
	if db == nil {
		return errHistoryStorage
	}
	return writeStoredBlockCheck(ctx, db, errHistoryStorage, func(tx *gorm.DB) error {
		return writeHistoryRun(tx, run)
	})
}

func migrateHistory(tx *gorm.DB, now time.Time) error {
	if err := tx.AutoMigrate(&historyRunRow{}, &historyGroupRow{}); err != nil {
		return err
	}
	legacy, err := catalogSetting(tx, feature.HistorySettingKey)
	if err != nil {
		return err
	}
	if legacy != "" {
		var rows int64
		if err := tx.Model(&historyRunRow{}).Count(&rows).Error; err != nil {
			return errHistoryStorage
		}
		if rows != 0 {
			return feature.ErrInvalidHistory
		}
		old, err := feature.DecodeHistory([]byte(legacy), now)
		if err != nil {
			return err
		}
		for _, run := range old.Runs {
			if err := writeHistoryRun(tx, run); err != nil {
				return err
			}
		}
		if err := tx.Where("key = ?", feature.HistorySettingKey).Delete(&model.Setting{}).Error; err != nil {
			return errHistoryStorage
		}
	}
	return pruneHistory(tx, now)
}
