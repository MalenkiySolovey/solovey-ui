// Package devices supplies Product diagnostics to the canonical VPN user device owner.
package devices

import (
	"context"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	owner "github.com/MalenkiySolovey/solovey-ui/backend/users/devices"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

const Field = owner.Field

var (
	ErrInvalid     = owner.ErrInvalid
	ErrUnavailable = owner.ErrUnavailable
	ErrLimit       = owner.ErrLimit
	ErrConflict    = owner.ErrConflict
	ErrPool        = owner.ErrPool
)

type Grant = owner.Grant
type Record = owner.Record
type Observation = owner.Observation
type State = owner.State
type Info = owner.Info
type View = owner.View
type Policy = owner.Policy
type ResolvePolicy = owner.ResolvePolicy
type Request = owner.Request
type TrafficDelta = owner.TrafficDelta

func Active(c model.Client, now int64) bool { return owner.Active(c, now) }
func AvailableRecord(s State, r Record, c model.Client, now int64) bool {
	return owner.AvailableRecord(s, r, c, now)
}
func Preserve(proposed, stored map[string]json.RawMessage, id uint) error {
	return owner.Preserve(proposed, stored, id)
}
func DesiredConfig(raw []byte) []byte                        { return owner.DesiredConfig(raw) }
func Project(id uint, raw []byte) ([]byte, error)            { return owner.Project(id, raw) }
func Inspect(c model.Client, now int64) View                 { return owner.Inspect(c, now) }
func Load(tx *gorm.DB, id uint) (model.Client, State, error) { return owner.Load(tx, id) }
func DecodeRequest(raw []byte) (Request, error)              { return owner.DecodeRequest(raw) }
func Mutate(tx *gorm.DB, r Request, resolve ResolvePolicy, now int64) ([]uint, error) {
	return owner.Mutate(tx, r, resolve, now, logger.Warning)
}
func RuntimePeers(ctx context.Context, tx *gorm.DB, p Policy, now int64) ([]map[string]any, error) {
	return owner.RuntimePeers(ctx, tx, p, now, logger.Warning)
}
func WithPrivateKeys(ctx context.Context, tx *gorm.DB, id uint, device string, now int64, use func(Info, []byte, []byte) error) error {
	return owner.WithPrivateKeys(ctx, tx, id, device, now, use, logger.Warning)
}
func EndpointIDs(raw []byte) []uint { return owner.EndpointIDs(raw) }
func Account(tx *gorm.DB, endpoint uint, generation string, now int64, samples map[string]Observation) ([]TrafficDelta, error) {
	return owner.Account(tx, endpoint, generation, now, samples)
}
