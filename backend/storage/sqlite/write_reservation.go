package sqlite

import (
	"errors"

	"gorm.io/gorm"
)

// ReserveWrite admits an existing owner transaction before its first read.
// SQLite cannot wait when upgrading an obsolete WAL read snapshot to a writer;
// a later credential audit would fail even though busy_timeout is configured.
// The zero-row update reserves the writer without changing desired state. It
// retains GORM's transaction, pool and maintenance admission, and must precede
// all snapshot reads. Ordinary read-only transactions need no reservation.
func ReserveWrite(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil {
		return errors.New("database write reservation requires a transaction")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("database write reservation requires a transaction")
	}
	return tx.Exec("UPDATE settings SET value = value WHERE 0").Error
}
