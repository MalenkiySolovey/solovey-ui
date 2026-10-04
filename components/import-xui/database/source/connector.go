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
	if err := restrictSourceConnection(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("source connection restrictions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func restrictSourceConnection(conn driver.Conn) error {
	for _, query := range []string{"PRAGMA query_only=ON", "PRAGMA trusted_schema=OFF"} {
		stmt, err := conn.Prepare(query)
		if err != nil {
			return err
		}
		_, execErr := stmt.Exec(nil)
		closeErr := stmt.Close()
		if execErr != nil {
			return execErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
