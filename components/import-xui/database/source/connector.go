//go:build !minimal

package source

import (
	"context"
	"database/sql/driver"
	"fmt"

	"github.com/mattn/go-sqlite3"
)

// sourceConnector applies connection-local restrictions on every connection,
// including replacements opened after the pool discards a connection.
type sourceConnector struct{ dsn string }

func (c sourceConnector) Driver() driver.Driver { return &sqlite3.SQLiteDriver{} }

func (c sourceConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, err
	}
	if conn == nil {
		return nil, ErrSourceUnavailable
	}
	if err := restrictSourceConnection(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("source connection restrictions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func restrictSourceConnection(ctx context.Context, conn driver.Conn) error {
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		return fmt.Errorf("source driver does not support context execution")
	}
	for _, query := range []string{"PRAGMA query_only=ON", "PRAGMA trusted_schema=OFF"} {
		if _, err := execer.ExecContext(ctx, query, nil); err != nil {
			return err
		}
	}
	return nil
}
