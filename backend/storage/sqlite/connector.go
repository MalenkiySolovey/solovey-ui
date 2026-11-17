package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// Each pool gets its own connector/generation. No process-wide SQLite driver
// registration or replacement is needed. Count an open before SQLite starts
// touching the file, and retain it until the real driver Close succeeds.
type generationConnector struct {
	dsn        string
	generation *generation
}

func (c *generationConnector) Driver() driver.Driver { return &sqlite3.SQLiteDriver{} }
func (c *generationConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dbMu.Lock()
	if err := c.generation.permittedLocked(ctx); err != nil {
		dbMu.Unlock()
		return nil, err
	}
	c.generation.connections++
	changedLocked()
	dbMu.Unlock()
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		var closeErr error
		if conn != nil {
			closeErr = conn.Close()
		}
		c.finishOpen(closeErr)
		return nil, errors.Join(err, closeErr)
	}
	if err := ctx.Err(); err != nil {
		closeErr := conn.Close()
		c.finishOpen(closeErr)
		return nil, errors.Join(err, closeErr)
	}
	return &generationConn{conn: conn, generation: c.generation}, nil
}
func (c *generationConnector) finishOpen(closeErr error) {
	dbMu.Lock()
	defer dbMu.Unlock()
	if closeErr == nil {
		c.generation.connections--
	} else {
		c.generation.closeErr = errors.Join(c.generation.closeErr, closeErr)
	}
	changedLocked()
}

// Forward the pinned driver's context-aware capabilities. In particular do
// not add SessionResetter or Validator: neither exists on pinned SQLiteConn,
// and adding them changes database/sql's transaction-cancellation semantics.
// Keep the concrete driver private; Raw callbacks get the guarded adapter.
type generationConn struct {
	conn       driver.Conn
	generation *generation
	closeOnce  sync.Once
	closeErr   error
}

func (c *generationConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close()
		dbMu.Lock()
		if c.closeErr == nil {
			c.generation.connections--
		} else {
			c.generation.closeErr = errors.Join(c.generation.closeErr, c.closeErr)
		}
		changedLocked()
		dbMu.Unlock()
	})
	return c.closeErr
}
func (c *generationConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *generationConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.generation.permitted(ctx); err != nil {
		return nil, err
	}
	var stmt driver.Stmt
	var err error
	if conn, ok := c.conn.(driver.ConnPrepareContext); ok {
		stmt, err = conn.PrepareContext(ctx, query)
	} else {
		stmt, err = c.conn.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &generationStmt{stmt: stmt, generation: c.generation}, nil
}
func (c *generationConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *generationConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if err := c.generation.permitted(ctx); err != nil {
		return nil, err
	}
	if conn, ok := c.conn.(driver.ConnBeginTx); ok {
		return conn.BeginTx(ctx, options)
	}
	return nil, errors.New("SQLite transaction context unavailable")
}
func (c *generationConn) Ping(ctx context.Context) error {
	if err := c.generation.permitted(ctx); err != nil {
		return err
	}
	if conn, ok := c.conn.(driver.Pinger); ok {
		return conn.Ping(ctx)
	}
	return nil
}
func (c *generationConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	return c.ExecContext(context.Background(), query, namedValues(args))
}
func (c *generationConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.generation.permitted(ctx); err != nil {
		return nil, err
	}
	if conn, ok := c.conn.(driver.ExecerContext); ok {
		return conn.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}
func (c *generationConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	return c.QueryContext(context.Background(), query, namedValues(args))
}
func (c *generationConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.generation.permitted(ctx); err != nil {
		return nil, err
	}
	if conn, ok := c.conn.(driver.QueryerContext); ok {
		return conn.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

type generationStmt struct {
	stmt       driver.Stmt
	generation *generation
}

func (s *generationStmt) Close() error  { return s.stmt.Close() }
func (s *generationStmt) NumInput() int { return s.stmt.NumInput() }
func (s *generationStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(args))
}
func (s *generationStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.generation.permitted(ctx); err != nil {
		return nil, err
	}
	if stmt, ok := s.stmt.(driver.StmtExecContext); ok {
		return stmt.ExecContext(ctx, args)
	}
	return nil, errors.New("SQLite statement execution unavailable")
}
func (s *generationStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(args))
}
func (s *generationStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.generation.permitted(ctx); err != nil {
		return nil, err
	}
	if stmt, ok := s.stmt.(driver.StmtQueryContext); ok {
		return stmt.QueryContext(ctx, args)
	}
	return nil, errors.New("SQLite statement query unavailable")
}
func namedValues(values []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(values))
	for index, value := range values {
		named[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return named
}

var (
	_ driver.Connector          = (*generationConnector)(nil)
	_ driver.ConnPrepareContext = (*generationConn)(nil)
	_ driver.ConnBeginTx        = (*generationConn)(nil)
	_ driver.Pinger             = (*generationConn)(nil)
	_ driver.ExecerContext      = (*generationConn)(nil)
	_ driver.QueryerContext     = (*generationConn)(nil)
	_ driver.StmtExecContext    = (*generationStmt)(nil)
	_ driver.StmtQueryContext   = (*generationStmt)(nil)
)
