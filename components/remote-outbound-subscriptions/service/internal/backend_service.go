//go:build !minimal

package remotesubservice

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/backendoutbounds"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	featuredomain "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
	feature "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/service"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
	"time"
)

type backendStore struct {
	featuredomain.Store
	db *gorm.DB
}

func serviceStore(db *gorm.DB) *backendStore {
	return &backendStore{Store: remotesub.BackendStore(db), db: db}
}
func (b *backendStore) WithContext(ctx context.Context) feature.Store {
	return serviceStore(b.db.WithContext(ctx))
}
func (b *backendStore) OperationContext(ctx context.Context) context.Context {
	return backendoutbounds.OperationContext(ctx, b.db)
}
func (b *backendStore) Transaction(call func(feature.Store) error) error {
	return b.db.Transaction(func(tx *gorm.DB) error { return call(serviceStore(tx)) })
}
func (b *backendStore) AcquireOperation(ctx context.Context) (context.Context, func(), error) {
	return dbsqlite.AcquireOperation(ctx)
}
func (b *backendStore) ValidateDownloadOutbound(id, previous uint) error {
	return validateDownloadOutbound(b.db, id, previous)
}

type storeError struct{ cause error }

func (e storeError) Error() string { return e.cause.Error() }
func (e storeError) Unwrap() error { return e.cause }
func (e storeError) Is(target error) bool {
	return target == featuredomain.ErrRecordNotFound || errors.Is(e.cause, target)
}
func backendError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storeError{err}
	}
	return err
}

func (b *backendStore) ListSubscriptionDetails(out *[]remotesub.RemoteOutboundSubscription) error {
	return backendError(func() error {
		return b.db.Preload("Groups", func(db *gorm.DB) *gorm.DB { return db.Order(entityorder.Clause) }).Preload("Connections", func(db *gorm.DB) *gorm.DB { return db.Order(entityorder.Clause) }).Order(entityorder.Clause).Find(out).Error
	}())
}

func (b *backendStore) SubscriptionSaveOrder(id uint) (int, error) {
	return entityorder.ForSave(b.db, &remotesub.RemoteOutboundSubscription{}, id)
}

func (b *backendStore) SaveSubscription(value *remotesub.RemoteOutboundSubscription) error {
	return backendError(func() error { return b.db.Save(value).Error }())
}

func (b *backendStore) LoadSubscriptionDetails(id uint, out *remotesub.RemoteOutboundSubscription) error {
	return backendError(func() error {
		return b.db.Preload("Groups", func(db *gorm.DB) *gorm.DB { return db.Order(entityorder.Clause) }).Preload("Connections", func(db *gorm.DB) *gorm.DB { return db.Order(entityorder.Clause) }).First(out, id).Error
	}())
}

func (b *backendStore) DeleteSubscriptionMemberships(subscriptionID uint) error {
	return backendError(func() error {
		return b.db.Where("connection_id IN (SELECT id FROM remote_outbound_connections WHERE subscription_id = ?)", subscriptionID).Delete(&remotesub.RemoteOutboundGroupConnection{}).Error
	}())
}

func (b *backendStore) DeleteSubscriptionConnections(subscriptionID uint) error {
	return backendError(func() error {
		return b.db.Where("subscription_id = ?", subscriptionID).Delete(&remotesub.RemoteOutboundConnection{}).Error
	}())
}

func (b *backendStore) DeleteSubscriptionGroups(subscriptionID uint) error {
	return backendError(func() error {
		return b.db.Where("subscription_id = ?", subscriptionID).Delete(&remotesub.RemoteOutboundGroup{}).Error
	}())
}

func (b *backendStore) DeleteSubscription(subscriptionID uint) error {
	return backendError(func() error { return b.db.Delete(&remotesub.RemoteOutboundSubscription{}, subscriptionID).Error }())
}

func (b *backendStore) CountSubscription(id uint, out *int64) error {
	return backendError(func() error {
		return b.db.Model(&remotesub.RemoteOutboundSubscription{}).Where("id = ?", id).Count(out).Error
	}())
}

func (b *backendStore) GroupSaveOrder(id uint) (int, error) {
	return entityorder.ForSave(b.db, &remotesub.RemoteOutboundGroup{}, id)
}

func (b *backendStore) ListOrderedSubscriptions(out *[]remotesub.RemoteOutboundSubscription) error {
	return backendError(func() error { return b.db.Order(entityorder.Clause).Find(out).Error }())
}

func (b *backendStore) CountNamedGroup(subscriptionID uint, name string, out *int64) error {
	return backendError(func() error {
		return b.db.Model(&remotesub.RemoteOutboundGroup{}).Where("subscription_id = ? AND name = ?", subscriptionID, name).Count(out).Error
	}())
}

func (b *backendStore) MaxSubscriptionGroupOrder(subscriptionID uint, out *int) error {
	return backendError(func() error {
		return b.db.Model(&remotesub.RemoteOutboundGroup{}).Where("subscription_id = ?", subscriptionID).Select("COALESCE(MAX(sort_order), 0)").Scan(out).Error
	}())
}

func (b *backendStore) LoadCollectedSubscription(id uint, out *remotesub.RemoteOutboundSubscription) error {
	return backendError(func() error {
		return b.db.Preload("Connections", func(db *gorm.DB) *gorm.DB { return db.Order(entityorder.Clause) }).First(out, id).Error
	}())
}

func (b *backendStore) ListHealthSources(after uint, limit int, out *[]remotesub.RemoteOutboundSubscription) error {
	return backendError(func() error {
		return b.db.Select("id", "health_check").Where("enabled = ? AND json_extract(health_check, '$.enabled') = ? AND id > ?", true, true, after).Order("id").Limit(limit).Find(out).Error
	}())
}

func (b *backendStore) ListHealthTargets(subscriptionID, after uint, limit int, out *[]feature.SourceHealthTarget) error {
	return backendError(func() error {
		return b.db.Table("remote_outbound_connections AS c").Select("c.id, o.tag, o.type, o.options").Joins("JOIN outbounds AS o ON o.id = c.outbound_id AND o.tag = c.outbound_tag").Where("c.subscription_id = ? AND c.enabled = ? AND c.synced = ? AND c.id > ?", subscriptionID, true, true, after).Order("c.id").Limit(limit).Scan(out).Error
	}())
}

type Effects interface {
	RecordChange(*gorm.DB, string, string, any) error
	Invalidate(string, bool)
}
type Settings = feature.Settings
type Service struct {
	Effects       Effects
	Settings      Settings
	OutboundHTTP  componenthost.OutboundHTTPClientProvider
	ProbeOutbound func(context.Context, string, string, time.Duration) coreruntime.CheckOutboundResult
}

func (s *Service) implementation() *feature.Service {
	return &feature.Service{Store: serviceStore(dbsqlite.DB()), Effects: backendEffects{s}, Settings: s.Settings, Security: backendSecurity{}, Fetcher: backendFetcher{s}, Checks: backendChecker{}, LogWarning: logger.Warning, ProbeOutbound: func(ctx context.Context, tag, target string, timeout time.Duration) feature.ProbeResult {
		if s.ProbeOutbound == nil {
			return feature.ProbeResult{Error: "core_unavailable"}
		}
		return feature.ProbeResult{Error: s.ProbeOutbound(ctx, tag, target, timeout).Error}
	}}
}

type backendEffects struct{ s *Service }

func (e backendEffects) RecordChange(store feature.Store, login, act string, payload any) error {
	if e.s.Effects == nil {
		return errors.New("remote outbound effects are not configured")
	}
	return e.s.Effects.RecordChange(store.(*backendStore).db, login, act, payload)
}
func (e backendEffects) Invalidate(login string, restart bool) {
	if e.s.Effects != nil {
		e.s.Effects.Invalidate(login, restart)
	}
}

type backendSecurity struct{}

func (backendSecurity) PrepareDialerOverride(raw json.RawMessage) (json.RawMessage, error) {
	return coreruntime.PrepareDialerOverride(raw)
}
func (backendSecurity) PrepareSourceHealthCheck(raw json.RawMessage) (json.RawMessage, error) {
	return remotesub.PrepareSourceHealthCheck(raw)
}
func (backendSecurity) ParseSourceHealthCheck(raw json.RawMessage) (remotesub.SourceHealthCheck, error) {
	return remotesub.ParseSourceHealthCheck(raw)
}
func (backendSecurity) ValidateSubscriptionURL(raw string) error {
	return remotesub.ValidateSubscriptionURL(raw)
}
func (backendSecurity) ValidateSourceFilePath(raw string) error {
	return remotesub.ValidateSourceFilePath(raw)
}
func (backendSecurity) ApplySourceContent(ctx context.Context, store feature.Store, value *remotesub.RemoteOutboundSubscription, raw json.RawMessage, options remotesub.FetchOptions) error {
	return remotesub.ApplySourceContent(ctx, store.(*backendStore).db, value, raw, options)
}
func (backendSecurity) ApplySourceHeaders(store feature.Store, value *remotesub.RemoteOutboundSubscription, raw json.RawMessage) error {
	return remotesub.ApplySourceHeaders(store.(*backendStore).db, value, raw)
}
func (backendSecurity) ProjectSourceContent(value *remotesub.RemoteOutboundSubscription) error {
	return remotesub.ProjectSourceContent(value)
}
func (backendSecurity) OutboundAvailable(kind string, raw json.RawMessage) bool {
	return entitycapabilities.ResolveResource("outbounds", kind, raw).Available
}

type backendFetcher struct{ s *Service }

func (f backendFetcher) FetchSource(ctx context.Context, store feature.Store, source remotesub.RemoteOutboundSubscription, options remotesub.FetchOptions) (*remotesub.FetchedSubscription, error) {
	return f.s.fetchSourceWithOptions(ctx, store.(*backendStore).db, source, options)
}

type backendChecker struct{}

func (backendChecker) CheckConnection(ctx context.Context, id uint, target string) (*remotesub.CheckResult, error) {
	return remotesub.CheckConnection(ctx, dbsqlite.DB(), id, target)
}
func (backendChecker) CheckSubscription(ctx context.Context, id uint, target string) ([]remotesub.CheckResult, error) {
	return remotesub.CheckSubscription(ctx, dbsqlite.DB(), id, target)
}
func (backendChecker) CheckAll(ctx context.Context, target string) ([]remotesub.CheckResult, error) {
	return remotesub.CheckAll(ctx, dbsqlite.DB(), target)
}
