package sqlite

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func dropDeprecatedTables() error {
	for _, query := range []string{
		"DROP TABLE IF EXISTS xui_sync_profiles",
		"DROP TABLE IF EXISTS xui_known_hosts",
	} {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureNoTLSRow() error {
	return EnsureNoTLSRow(db)
}

func EnsureNoTLSRow(target *gorm.DB) error {
	if target == nil {
		return nil
	}
	return target.Exec(
		"INSERT OR IGNORE INTO tls(id, name, server, client) VALUES(0, ?, ?, ?)",
		"__none__", []byte("{}"), []byte("{}"),
	).Error
}

func ensureIndexes() error {
	if err := db.Exec("DROP INDEX IF EXISTS idx_client_ips_client_ip").Error; err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM settings WHERE id NOT IN (SELECT MIN(id) FROM settings GROUP BY key)").Error; err != nil {
		return err
	}
	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_settings_key ON settings(key)",
		"CREATE INDEX IF NOT EXISTS idx_stats_lookup ON stats(date_time, resource, tag)",
		"CREATE INDEX IF NOT EXISTS idx_stats_resource_tag_dt ON stats(resource, tag, date_time)",
		"CREATE INDEX IF NOT EXISTS idx_stats_resource_dt ON stats(resource, date_time)",
		"CREATE INDEX IF NOT EXISTS idx_changes_lookup ON changes(date_time, actor, key)",
		"CREATE INDEX IF NOT EXISTS idx_audit_events_lookup ON audit_events(date_time, actor, event)",
		"CREATE INDEX IF NOT EXISTS idx_audit_events_event_dt ON audit_events(event, date_time DESC)",
		"CREATE INDEX IF NOT EXISTS idx_audit_events_severity_dt ON audit_events(severity, date_time DESC)",
		"CREATE INDEX IF NOT EXISTS idx_clients_name ON clients(name)",
		"CREATE INDEX IF NOT EXISTS idx_clients_sub_secret ON clients(sub_secret)",
		"CREATE INDEX IF NOT EXISTS idx_clients_sort_order ON clients(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_inbounds_sort_order ON inbounds(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_outbounds_sort_order ON outbounds(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_subscriptions_sort_order ON remote_outbound_subscriptions(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_groups_subscription_sort_order ON remote_outbound_groups(subscription_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_group_connections_group ON remote_outbound_group_connections(group_id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_group_connections_connection ON remote_outbound_group_connections(connection_id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_subscription_sort_order ON remote_outbound_connections(subscription_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_group_sort_order ON remote_outbound_connections(group_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_outbound_id ON remote_outbound_connections(outbound_id)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_sort_order ON endpoints(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_services_sort_order ON services(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_tls_sort_order ON tls(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_users_sort_order ON users(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_client_ips_client_legacy_ip ON client_ips(client_name, ip) WHERE ip IS NOT NULL AND ip != ''",
		"CREATE INDEX IF NOT EXISTS idx_client_ips_last_seen ON client_ips(last_seen)",
	}
	for _, query := range indexes {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureSortOrders() error {
	for _, table := range []string{
		"inbounds", "clients", "outbounds", "remote_outbound_subscriptions",
		"remote_outbound_groups", "remote_outbound_connections", "endpoints",
		"services", "tls", "users",
	} {
		if err := ensureTableSortOrder(table); err != nil {
			return err
		}
	}
	return nil
}

func ensureTableSortOrder(table string) error {
	if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, "sort_order") {
		return nil
	}

	quotedTable := quoteSQLiteIdentifier(table)
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

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
