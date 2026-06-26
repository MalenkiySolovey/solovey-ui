package source

import (
	"database/sql"
	"errors"
)

var ErrDialectUnknown = errors.New("xui_dialect_unknown")

type Dialect interface {
	Name() string
	Detect(db *sql.DB) (bool, error)
	ReadInbounds(db *sql.DB) ([]InboundRow, error)
	ReadClients(db *sql.DB) ([]ClientTraffic, error)
	ReadSettings(db *sql.DB) ([]Setting, error)
	ReadUsers(db *sql.DB) ([]User, error)
	ReadOutboundTraffics(db *sql.DB) ([]OutboundTraffic, error)
	ReadXrayConfig(db *sql.DB) (string, error)
}

func RegisteredDialects() []Dialect {
	return []Dialect{Dialect3XUIMHSanaei{}}
}
