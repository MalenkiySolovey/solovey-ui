package audit

import "github.com/MalenkiySolovey/solovey-ui/database/model"

// History queries existing redacted events; it grants no write or prune authority.
type History interface {
	ListPageFiltered(uint64, int, string, string, int64, int64) ([]model.AuditEvent, uint64, error)
}
