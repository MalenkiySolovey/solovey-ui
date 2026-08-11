//go:build !minimal

package remote

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/sqliteident"

	"gorm.io/gorm"
)

func EnsureSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := db.AutoMigrate(
		&RemoteOutboundSubscription{},
		&RemoteOutboundGroup{},
		&RemoteOutboundGroupConnection{},
		&RemoteOutboundConnection{},
	); err != nil {
		return err
	}
	if err := dropConnectionMissingColumns(db); err != nil {
		return err
	}
	for _, query := range []string{
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_subscriptions_sort_order ON remote_outbound_subscriptions(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_groups_subscription_sort_order ON remote_outbound_groups(subscription_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_group_connections_group ON remote_outbound_group_connections(group_id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_group_connections_connection ON remote_outbound_group_connections(connection_id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_subscription_sort_order ON remote_outbound_connections(subscription_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_group_sort_order ON remote_outbound_connections(group_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_outbound_id ON remote_outbound_connections(outbound_id)",
	} {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	for _, table := range []string{
		"remote_outbound_subscriptions",
		"remote_outbound_groups",
		"remote_outbound_connections",
	} {
		if err := ensureTableSortOrder(db, table); err != nil {
			return err
		}
	}
	return nil
}

func DropSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	migrator := db.Migrator()
	for _, table := range []any{
		&RemoteOutboundGroupConnection{},
		&RemoteOutboundConnection{},
		&RemoteOutboundGroup{},
		&RemoteOutboundSubscription{},
	} {
		if !migrator.HasTable(table) {
			continue
		}
		if err := migrator.DropTable(table); err != nil {
			return err
		}
	}
	return nil
}

func dropConnectionMissingColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable(&RemoteOutboundConnection{}) {
		return nil
	}
	for _, column := range []string{"missing", "missing_reason", "missing_since"} {
		if !db.Migrator().HasColumn(&RemoteOutboundConnection{}, column) {
			continue
		}
		if err := db.Migrator().DropColumn(&RemoteOutboundConnection{}, column); err != nil {
			return fmt.Errorf("drop remote connection column %s: %w", column, err)
		}
	}
	return nil
}

func ensureTableSortOrder(db *gorm.DB, table string) error {
	if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, "sort_order") {
		return nil
	}
	quotedTable := sqliteident.Quote(table)
	rows := []struct {
		ID        int64
		SortOrder int
	}{}
	if err := db.Raw(fmt.Sprintf("SELECT id, sort_order FROM %s ORDER BY sort_order ASC, id ASC", quotedTable)).Scan(&rows).Error; err != nil {
		return err
	}
	needsBackfill := false
	for index, row := range rows {
		if row.SortOrder != index+1 {
			needsBackfill = true
			break
		}
	}
	if !needsBackfill {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		query := fmt.Sprintf("UPDATE %s SET sort_order = ? WHERE id = ?", quotedTable)
		for index, row := range rows {
			if err := tx.Exec(query, index+1, row.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
