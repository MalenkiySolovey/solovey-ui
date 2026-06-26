package integrity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

type foreignKeyViolation struct {
	Table  string
	RowID  int64
	Parent string
	FKID   int
}

type Options struct {
	RepairForeignKeyOrphans bool
}

func VerifyForeignKeysBeforeMigration(db *gorm.DB, options Options) error {
	violations, err := foreignKeyViolations(db)
	if err != nil {
		return fmt.Errorf("foreign key check: %w", err)
	}
	if len(violations) == 0 {
		return nil
	}
	fmt.Println("Foreign key check failed:", summarizeForeignKeyViolations(violations))
	if err := recordForeignKeyAudit(db, violations, false); err != nil {
		fmt.Println("Warning: foreign-key audit event skipped:", err)
	}
	if !options.RepairForeignKeyOrphans {
		return fmt.Errorf("foreign key check failed: %s; rerun `solovey-ui migrate -repair-fk-orphans` to delete safe token orphans, or repair the database manually", summarizeForeignKeyViolations(violations))
	}
	repaired, err := repairSafeForeignKeyOrphans(db, violations)
	if err != nil {
		return fmt.Errorf("repair foreign key orphans: %w", err)
	}
	remaining, err := foreignKeyViolations(db)
	if err != nil {
		return fmt.Errorf("foreign key recheck: %w", err)
	}
	if len(remaining) > 0 {
		_ = recordForeignKeyAudit(db, remaining, false)
		return fmt.Errorf("foreign key check still fails after deleting %d safe token orphans: %s; repair manually", repaired, summarizeForeignKeyViolations(remaining))
	}
	fmt.Printf("Foreign key repair deleted %d safe token orphan(s)\n", repaired)
	if err := recordForeignKeyAudit(db, violations, true); err != nil {
		fmt.Println("Warning: foreign-key repair audit event skipped:", err)
	}
	return nil
}

func EnsureNoTLSForeignKeyParent(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.Tls{}) ||
		!db.Migrator().HasColumn(&model.Tls{}, "server") ||
		!db.Migrator().HasColumn(&model.Tls{}, "client") {
		return nil
	}
	if err := db.Exec("INSERT OR IGNORE INTO tls(id, name, server, client) VALUES(0, ?, ?, ?)", "__none__", []byte("{}"), []byte("{}")).Error; err != nil {
		return fmt.Errorf("ensure no-tls foreign key parent: %w", err)
	}
	return nil
}

func foreignKeyViolations(db *gorm.DB) ([]foreignKeyViolation, error) {
	rows, err := db.Raw("PRAGMA foreign_key_check").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	violations := make([]foreignKeyViolation, 0)
	for rows.Next() {
		var row foreignKeyViolation
		if err := rows.Scan(&row.Table, &row.RowID, &row.Parent, &row.FKID); err != nil {
			return nil, err
		}
		violations = append(violations, row)
	}
	return violations, rows.Err()
}

func foreignKeyViolationCounts(violations []foreignKeyViolation) map[string]int {
	counts := make(map[string]int)
	for _, violation := range violations {
		counts[violation.Table]++
	}
	return counts
}

func summarizeForeignKeyViolations(violations []foreignKeyViolation) string {
	counts := foreignKeyViolationCounts(violations)
	parts := make([]string, 0, len(counts))
	for table, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", table, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func repairSafeForeignKeyOrphans(db *gorm.DB, violations []foreignKeyViolation) (int64, error) {
	counts := foreignKeyViolationCounts(violations)
	if counts["tokens"] == 0 {
		return 0, nil
	}
	if !db.Migrator().HasTable("tokens") || !db.Migrator().HasTable("users") {
		return 0, nil
	}
	result := db.Exec(`
DELETE FROM tokens
WHERE user_id IS NULL
	OR user_id NOT IN (SELECT id FROM users)
`)
	return result.RowsAffected, result.Error
}

func recordForeignKeyAudit(db *gorm.DB, violations []foreignKeyViolation, repaired bool) error {
	if !db.Migrator().HasTable("audit_events") {
		return nil
	}
	details, err := json.Marshal(map[string]any{
		"counts":   foreignKeyViolationCounts(violations),
		"repaired": repaired,
	})
	if err != nil {
		return err
	}
	return db.Create(&model.AuditEvent{
		DateTime: time.Now().Unix(),
		Actor:    "system",
		Event:    "foreign_key_check_failed",
		Resource: "database",
		Severity: "warn",
		Details:  details,
	}).Error
}
