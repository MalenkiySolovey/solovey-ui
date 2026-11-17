// Package credentials adapts Product diagnostics to canonical Users credential commands.
package credentials

import (
	"context"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	owner "github.com/MalenkiySolovey/solovey-ui/backend/users/credentials"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

var ErrInvalid = owner.ErrInvalid

func ValidateClientConfig(raw json.RawMessage) error { return owner.ValidateClientConfig(raw) }
func ProjectClient(id uint, raw json.RawMessage) (json.RawMessage, error) {
	return owner.ProjectClient(id, raw)
}
func ApplyClientField(tx *gorm.DB, id uint, kind, current string, mutation json.RawMessage) (string, error) {
	return owner.ApplyClientField(tx, id, kind, current, mutation, logger.Warning)
}
func UseClient(ctx context.Context, tx *gorm.DB, id uint, kind string, raw json.RawMessage, intent string, use func([]byte) error) error {
	return owner.UseClient(ctx, tx, id, kind, raw, intent, use, logger.Warning)
}
func Generate(kind string) (string, error)              { return owner.Generate(kind) }
func SudokuPublicKey(value string) (string, error)      { return owner.SudokuPublicKey(value) }
func GenerateSudokuMaster() (string, error)             { return owner.GenerateSudokuMaster() }
func NormalizeSudokuSplit(value string) (string, error) { return owner.NormalizeSudokuSplit(value) }
func SudokuSplitMatches(value, master string) bool      { return owner.SudokuSplitMatches(value, master) }
func SudokuSplitPublicKey(value string) (string, error) { return owner.SudokuSplitPublicKey(value) }
func SplitSudokuMaster(value string) (string, error)    { return owner.SplitSudokuMaster(value) }
func UseSudokuMaster(ctx context.Context, tx *gorm.DB, inbound model.Inbound, intent string, use func([]byte) error) error {
	return owner.UseSudokuMaster(ctx, tx, inbound, intent, use, logger.Warning)
}
func SudokuKeyBag(value string) (map[string]string, error) { return owner.SudokuKeyBag(value) }
