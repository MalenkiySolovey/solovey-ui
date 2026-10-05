package paid

import (
	"encoding/json"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// EnsureSchema creates the paid-subscription tables and indexes idempotently.
// The package owns these tables because the paid subscription module is still
// optional and should not leak schema details into HTTP/bot adapters.
func EnsureSchema(db *gorm.DB) error {
	return db.Transaction(ensureSchema)
}

func ensureSchema(db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS paidsub_provider_cursors (kind TEXT PRIMARY KEY, after_id INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS paidsub_invoice_cancellations (id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL, ref TEXT NOT NULL, completed INTEGER NOT NULL DEFAULT 0)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_paidsub_cancel_ref ON paidsub_invoice_cancellations(ref)`,
		`CREATE INDEX IF NOT EXISTS idx_paidsub_cancel_pending ON paidsub_invoice_cancellations(completed, id)`,
		`CREATE TABLE IF NOT EXISTS paidsub_bindings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER NOT NULL,
			tg_user_id INTEGER NOT NULL,
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS tariffs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			description TEXT,
			price INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'RUB',
			stars_amount INTEGER NOT NULL DEFAULT 0,
			add_days INTEGER NOT NULL DEFAULT 0,
			add_traffic_bytes INTEGER NOT NULL DEFAULT 0,
			sort INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS payment_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER NOT NULL,
			tariff_id INTEGER NOT NULL,
			provider TEXT NOT NULL,
			amount INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			telegram_user_id INTEGER NOT NULL DEFAULT 0,
			idempotency_key TEXT NOT NULL,
			provider_charge_id TEXT,
			provider_payload BLOB,
			external_url TEXT,
			created_at INTEGER NOT NULL DEFAULT 0,
			paid_at INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL DEFAULT 0,
			grant_add_days INTEGER NOT NULL DEFAULT 0,
			grant_traffic_bytes INTEGER NOT NULL DEFAULT 0,
			grant_snapshot INTEGER NOT NULL DEFAULT 0,
			granted_up INTEGER NOT NULL DEFAULT 0,
			granted_down INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_paidsub_bindings_client ON paidsub_bindings(client_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_paidsub_bindings_tg ON paidsub_bindings(tg_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tariffs_enabled_sort ON tariffs(enabled, sort)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_orders_client ON payment_orders(client_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_orders_pending_poll ON payment_orders(provider, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_orders_telegram ON payment_orders(telegram_user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_idem ON payment_orders(idempotency_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_charge ON payment_orders(provider, provider_charge_id) WHERE provider_charge_id != ''`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	mig := db.Migrator()
	for _, c := range []struct{ column, ddl string }{
		{"grant_add_days", `ALTER TABLE payment_orders ADD COLUMN grant_add_days INTEGER NOT NULL DEFAULT 0`},
		{"grant_traffic_bytes", `ALTER TABLE payment_orders ADD COLUMN grant_traffic_bytes INTEGER NOT NULL DEFAULT 0`},
		{"grant_snapshot", `ALTER TABLE payment_orders ADD COLUMN grant_snapshot INTEGER NOT NULL DEFAULT 0`},
		{"granted_up", `ALTER TABLE payment_orders ADD COLUMN granted_up INTEGER NOT NULL DEFAULT 0`},
		{"granted_down", `ALTER TABLE payment_orders ADD COLUMN granted_down INTEGER NOT NULL DEFAULT 0`},
		{"provider_ref", `ALTER TABLE payment_orders ADD COLUMN provider_ref TEXT NOT NULL DEFAULT ''`},
		{"review_reason", `ALTER TABLE payment_orders ADD COLUMN review_reason TEXT NOT NULL DEFAULT ''`},
		{"legacy_resolved", `ALTER TABLE payment_orders ADD COLUMN legacy_resolved INTEGER NOT NULL DEFAULT 0`},
		{"refund_revoke", `ALTER TABLE payment_orders ADD COLUMN refund_revoke INTEGER NOT NULL DEFAULT 0`},
		{"refund_claim", `ALTER TABLE payment_orders ADD COLUMN refund_claim TEXT NOT NULL DEFAULT ''`},
		{"refund_claim_until", `ALTER TABLE payment_orders ADD COLUMN refund_claim_until INTEGER NOT NULL DEFAULT 0`},
	} {
		if mig.HasColumn(&PaymentOrder{}, c.column) {
			continue
		}
		if err := db.Exec(c.ddl).Error; err != nil {
			return err
		}
	}
	// Reclassification may reveal old purchase/ref collisions. Drop only these
	// owned indexes within the migration transaction, quarantine, then recreate;
	// other writers never observe an unconstrained committed database.
	for _, ddl := range []string{"DROP INDEX IF EXISTS idx_payment_orders_active_purchase", "DROP INDEX IF EXISTS idx_payment_orders_ref"} {
		if err := db.Exec(ddl).Error; err != nil {
			return err
		}
	}
	if err := migrateOrders(db); err != nil {
		return err
	}
	for _, ddl := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_ref ON payment_orders(provider, provider_ref) WHERE provider_ref != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_active_purchase ON payment_orders(client_id, tariff_id, telegram_user_id, provider) WHERE provider = 'cryptobot' AND status IN ('pending','invoice_creating','recoverable')`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			return err
		}
	}
	return nil
}

// GrantSnapshot is the existing purchase proof. Migration never invents one
// from today's tariff, and never deletes the original financial evidence.
func migrateOrders(db *gorm.DB) error {
	// Older local TTL/failure transitions did not prove external nonpayment.
	// Only explicit terminal-provider evidence may keep these orders terminal.
	if err := db.Model(&PaymentOrder{}).Where("provider = 'cryptobot' AND legacy_resolved = 0 AND review_reason = '' AND status IN ?", []string{StatusExpired, StatusFailed}).
		Updates(map[string]any{"status": StatusRecoverable, "review_reason": "historical_provider_outcome_uncertain"}).Error; err != nil {
		return err
	}
	if err := db.Model(&PaymentOrder{}).Where("grant_snapshot = 0 AND legacy_resolved = 0 AND status = ?", StatusPaid).
		Updates(map[string]any{"status": StatusManualReview, "review_reason": "legacy_snapshot_missing"}).Error; err != nil {
		return err
	}
	if err := db.Model(&PaymentOrder{}).Where("grant_snapshot = 0 AND legacy_resolved = 0 AND provider = 'cryptobot' AND status IN ?", []string{StatusPending, StatusFailed, StatusExpired, StatusInvoiceCreating}).
		Updates(map[string]any{"status": StatusRecoverable, "review_reason": "legacy_snapshot_missing"}).Error; err != nil {
		return err
	}
	if err := db.Model(&PaymentOrder{}).Where("grant_snapshot = 0 AND legacy_resolved = 0 AND provider <> 'cryptobot' AND status IN ?", []string{StatusPending, StatusInvoiceCreating}).
		Updates(map[string]any{"status": StatusManualReview, "review_reason": "legacy_snapshot_missing"}).Error; err != nil {
		return err
	}
	// Bounded batches extract only already-stored invoice identity, never purchase
	// facts. Conflicting identities are quarantined before the unique index exists.
	var cursor uint
	for {
		var rows []PaymentOrder
		if err := db.Where("provider = 'cryptobot' AND id > ?", cursor).Order("id").Limit(256).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			cursor = row.Id
			if row.ProviderRef != "" || row.ReviewReason == "provider_identity_conflict" {
				continue
			}
			var saved struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(row.ProviderPayload, &saved)
			ref := saved.Ref
			if ref == "" && strings.HasPrefix(row.ProviderChargeID, "cryptobot:") {
				ref = strings.TrimPrefix(row.ProviderChargeID, "cryptobot:")
			}
			id, err := strconv.ParseInt(ref, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != ref {
				continue
			}
			if err := db.Model(&PaymentOrder{}).Where("id = ?", row.Id).Update("provider_ref", ref).Error; err != nil {
				return err
			}
		}
	}
	if err := db.Exec(`UPDATE payment_orders SET status = 'manual_review', review_reason = 'provider_identity_conflict', provider_ref = ''
		WHERE provider_ref != '' AND (provider, provider_ref) IN
		(SELECT provider, provider_ref FROM payment_orders WHERE provider_ref != '' GROUP BY provider, provider_ref HAVING COUNT(*) > 1)`).Error; err != nil {
		return err
	}
	return db.Exec(`UPDATE payment_orders SET status = 'manual_review', review_reason = 'active_purchase_conflict'
		WHERE provider = 'cryptobot' AND status IN ('pending','invoice_creating','recoverable') AND
		(client_id, tariff_id, telegram_user_id, provider) IN
		(SELECT client_id, tariff_id, telegram_user_id, provider FROM payment_orders WHERE provider = 'cryptobot'
		AND status IN ('pending','invoice_creating','recoverable') GROUP BY client_id, tariff_id, telegram_user_id, provider HAVING COUNT(*) > 1)`).Error
}

func DropSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	for _, table := range []string{
		"paidsub_invoice_cancellations",
		"paidsub_provider_cursors",
		"payment_orders",
		"tariffs",
		"paidsub_bindings",
	} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Migrator().DropTable(table); err != nil {
			return err
		}
	}
	return nil
}
