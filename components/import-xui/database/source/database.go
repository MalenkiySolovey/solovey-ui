//go:build !minimal

package source

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Database struct {
	db      *gorm.DB
	dialect Dialect
}

const MaxSourceBytes int64 = 200 << 20

type InboundRow struct {
	ID                   int64
	UserID               int64
	Up                   int64
	Down                 int64
	Total                int64
	AllTime              int64
	Remark               string
	Enable               bool
	ExpiryTime           int64
	TrafficReset         string
	LastTrafficResetTime int64
	Listen               string
	Port                 int
	Protocol             string
	Settings             json.RawMessage
	StreamSettings       json.RawMessage
	Tag                  string
	Sniffing             json.RawMessage
}

type ClientTraffic struct {
	ID         int64
	InboundID  int64
	Enable     bool
	Email      string
	Up         int64
	Down       int64
	AllTime    int64
	ExpiryTime int64
	Total      int64
	Reset      int64
	LastOnline int64
}

type Setting struct {
	ID    int64
	Key   string
	Value string
}

type OutboundTraffic struct {
	ID   int64
	Tag  string
	Up   int64
	Down int64
}

func Open(path string) (*Database, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("missing source path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxSourceBytes {
		return nil, fmt.Errorf("invalid source file")
	}
	dsn, err := SQLiteReadOnlyURI(path)
	if err != nil {
		return nil, err
	}
	sqlDB := sql.OpenDB(sourceConnector{dsn: dsn})
	opened := false
	defer func() {
		if !opened {
			_ = sqlDB.Close()
		}
	}()
	db, err := gorm.Open(gormsqlite.New(gormsqlite.Config{Conn: sqlDB}), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return nil, err
	}
	src := &Database{db: db}
	if err := src.validate(); err != nil {
		return nil, err
	}
	opened = true
	return src, nil
}

func SQLiteReadOnlyURI(path string) (string, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(path)), "file:") || strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("source must be a filesystem path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	urlPath := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	u := url.URL{
		Scheme: "file",
		Path:   urlPath,
	}
	values := url.Values{}
	values.Set("mode", "ro")
	values.Set("immutable", "1")
	values.Set("_query_only", "true")
	u.RawQuery = values.Encode()
	return u.String(), nil
}

func (s *Database) Close() {
	sqlDB, err := s.sqlDB()
	if err == nil && sqlDB != nil {
		_ = sqlDB.Close()
	}
}

func (s *Database) validate() error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}
	for _, dialect := range RegisteredDialects() {
		ok, err := dialect.Detect(sqlDB)
		if err != nil {
			return err
		}
		if ok {
			s.dialect = dialect
			var result string
			if err := sqlDB.QueryRow("PRAGMA quick_check(1)").Scan(&result); err != nil {
				return err
			}
			if result != "ok" {
				return fmt.Errorf("invalid sqlite integrity")
			}
			return nil
		}
	}
	return ErrDialectUnknown
}

func Hash(path string) (string, error) {
	// #nosec G304 -- path is an operator-supplied import source file validated by the caller.
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (s *Database) EachInbound(fn func(InboundRow) error) error {
	sqlDB, err := s.readDB()
	if err != nil {
		return err
	}
	if fn == nil {
		return errors.New("source inbound callback is unavailable")
	}
	rows, err := s.dialect.ReadInbounds(sqlDB)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := fn(row); err != nil {
			return err
		}
	}
	return nil
}

func (s *Database) EachClientTraffic(fn func(ClientTraffic) error) error {
	if fn == nil {
		return errors.New("source client callback is unavailable")
	}
	rows, err := s.Clients()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := fn(row); err != nil {
			return err
		}
	}
	return nil
}

func (s *Database) Clients() ([]ClientTraffic, error) {
	sqlDB, err := s.readDB()
	if err != nil {
		return nil, err
	}
	return s.dialect.ReadClients(sqlDB)
}

func (s *Database) InboundCount() (int, error) {
	sqlDB, err := s.readDB()
	if err != nil {
		return 0, err
	}
	rows, err := s.dialect.ReadInbounds(sqlDB)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *Database) Settings() ([]Setting, error) {
	sqlDB, err := s.readDB()
	if err != nil {
		return nil, err
	}
	return s.dialect.ReadSettings(sqlDB)
}

type User struct {
	ID       int64
	Username string
	Password string
}

func (s *Database) Users() ([]User, error) {
	sqlDB, err := s.readDB()
	if err != nil {
		return nil, err
	}
	return s.dialect.ReadUsers(sqlDB)
}

func (s *Database) OutboundTraffics() ([]OutboundTraffic, error) {
	sqlDB, err := s.readDB()
	if err != nil {
		return nil, err
	}
	return s.dialect.ReadOutboundTraffics(sqlDB)
}

func (s *Database) XrayConfig() (string, error) {
	sqlDB, err := s.readDB()
	if err != nil {
		return "", err
	}
	return s.dialect.ReadXrayConfig(sqlDB)
}

var ErrSourceUnavailable = errors.New("source database is unavailable")

func (s *Database) sqlDB() (*sql.DB, error) {
	if s == nil || s.db == nil || s.db.Config == nil {
		return nil, ErrSourceUnavailable
	}
	if s.db.Error != nil {
		return nil, fmt.Errorf("source database: %w", s.db.Error)
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil, fmt.Errorf("source database handle: %w", err)
	}
	if sqlDB == nil {
		return nil, ErrSourceUnavailable
	}
	return sqlDB, nil
}

func (s *Database) readDB() (*sql.DB, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return nil, err
	}
	if s.dialect == nil {
		return nil, ErrDialectUnknown
	}
	return sqlDB, nil
}

func nullString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func nullJSON(v sql.NullString) json.RawMessage {
	value := strings.TrimSpace(nullString(v))
	if value == "" {
		return nil
	}
	return json.RawMessage(value)
}

// ValidateSQLiteSource confirms that path is a readable, integrity-clean SQLite
// file in a recognised x-ui dialect before it is used as an import source.
func ValidateSQLiteSource(path string) error {
	// #nosec G304 -- path is an operator-supplied import source file validated by the caller.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	ok, err := backup.IsSQLite(file)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("not_sqlite")
	}
	src, err := Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	sqlDB, err := src.readDB()
	if err != nil {
		return err
	}
	var result string
	if err := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return errors.New("invalid sqlite integrity")
	}
	return nil
}
