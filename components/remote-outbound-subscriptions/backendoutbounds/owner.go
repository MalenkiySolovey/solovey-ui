//go:build !minimal

// Package backendoutbounds adapts Backend-owned transactions to one admitted
// source binding. Source data is read-only here; Outbounds retains every write.
package backendoutbounds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	hostcore "github.com/MalenkiySolovey/solovey-host/coreruntime"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"gorm.io/gorm"
)

// Source is the finite source-owner projection admitted by the Backend. Zero-ID
// values are supported for existing transaction-local creation, never lookup.
type Source struct {
	Id             uint
	SubscriptionId uint
	SourceKey      string
	Name           string
	Type           string
	OutboundTag    string
	OutboundId     *uint
	Enabled        bool
	Options        json.RawMessage
	Canonical      json.RawMessage
}

type providerObservationKey struct{}

// WithProviderObservation supplies the composition's trusted provider observer.
// It is not exposed through HTTP, feature ports or user-controlled settings.
func WithProviderObservation(ctx context.Context, observe func() coreprovider.Identity) context.Context {
	return context.WithValue(ctx, providerObservationKey{}, observe)
}

// OperationContext retains the trusted composition observer while taking all
// cancellation and deadlines from the current operation. It never reuses the
// database's background context as execution authority.
func OperationContext(ctx context.Context, db *gorm.DB) context.Context {
	if observe, ok := db.Statement.Context.Value(providerObservationKey{}).(func() coreprovider.Identity); ok && observe != nil {
		return WithProviderObservation(ctx, observe)
	}
	return ctx
}

type Owner struct {
	tx     *gorm.DB
	source Source
}

func New(tx *gorm.DB, source Source) *Owner { return &Owner{tx: tx, source: source} }
func (o *Owner) provider(ctx context.Context) coreprovider.Identity {
	if observe, ok := ctx.Value(providerObservationKey{}).(func() coreprovider.Identity); ok && observe != nil {
		return observe()
	}
	return coreprovider.Current()
}
func (o *Owner) BindingRef() hostoutbounds.BindingRef { return o.ref() }
func (o *Owner) ref() hostoutbounds.BindingRef {
	endpoint := strconv.FormatUint(uint64(o.source.Id), 10)
	if o.source.Id == 0 {
		endpoint = o.source.OutboundTag
	}
	return hostoutbounds.BindingRef{Host: hostref.Local(), SourceID: strconv.FormatUint(uint64(o.source.SubscriptionId), 10), EndpointID: endpoint}
}
func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func outboundID(id *uint) uint {
	if id == nil {
		return 0
	}
	return *id
}
func sourceDigest(s Source) string {
	s.OutboundId = nil // binding identity is fenced separately from source content
	return digest(s)
}
func resourceRevision(row model.Outbound) string {
	if row.Id == 0 {
		return "ABSENT"
	}
	type stored model.Outbound // include fixed fields omitted by runtime MarshalJSON
	return digest(struct {
		Stored  stored
		Options json.RawMessage
	}{stored(row), row.Options})
}
func (o *Owner) sourceRevision(ctx context.Context) (string, error) {
	if o.tx == nil {
		return "", hostoutbounds.ErrLease
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	current := o.source
	if current.Id != 0 {
		if err := o.tx.WithContext(ctx).Table("remote_outbound_connections").Where("id = ?", current.Id).First(&current).Error; err != nil {
			return "", hostoutbounds.ErrRevision
		}
		if sourceDigest(current) != sourceDigest(o.source) || outboundID(current.OutboundId) != outboundID(o.source.OutboundId) {
			return "", hostoutbounds.ErrRevision
		}
	}
	parent := ""
	if current.SubscriptionId != 0 {
		var row map[string]any
		if err := o.tx.WithContext(ctx).Table("remote_outbound_subscriptions").Where("id = ?", current.SubscriptionId).Take(&row).Error; err != nil {
			return "", hostoutbounds.ErrRevision
		}
		parent = digest(row)
	}
	return digest(struct{ Connection, Parent string }{sourceDigest(current), parent}), nil
}
func (o *Owner) resource(ctx context.Context) (model.Outbound, bool, error) {
	var row model.Outbound
	tx := o.tx.WithContext(ctx)
	if o.source.OutboundId != nil && *o.source.OutboundId != 0 {
		err := tx.First(&row, *o.source.OutboundId).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return row, false, err
		}
	}
	if row.Id == 0 {
		err := tx.Where("tag = ?", o.source.OutboundTag).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return row, false, err
		}
	}
	owned := row.Id == 0 || o.source.OutboundId != nil && row.Id == *o.source.OutboundId && row.Tag == o.source.OutboundTag
	if row.Id != 0 {
		var others int64
		if o.tx.Migrator().HasTable("remote_outbound_connections") {
			if err := tx.Table("remote_outbound_connections").Where("outbound_id = ? AND id <> ?", row.Id, o.source.Id).Count(&others).Error; err != nil {
				return row, false, err
			}
		}
		owned = owned && others == 0
	}
	return row, owned, nil
}
func (o *Owner) Prepare(ctx context.Context, action hostoutbounds.Action, definition json.RawMessage, missing hostoutbounds.Missing) (hostoutbounds.Candidate, error) {
	revision, err := o.sourceRevision(ctx)
	if err != nil {
		return hostoutbounds.Candidate{}, err
	}
	row, _, err := o.resource(ctx)
	if err != nil {
		return hostoutbounds.Candidate{}, err
	}
	provider := o.provider(ctx)
	if !coreprovider.SourceIdentityVerified(provider) {
		return hostoutbounds.Candidate{}, hostoutbounds.ErrProvider
	}
	return hostoutbounds.Candidate{Binding: o.ref(), Resource: hostoutbounds.ResourceRef{Host: hostref.Local(), ID: row.Id, Tag: o.source.OutboundTag}, Provider: hostcore.Provider{ID: provider.ID, Revision: provider.Revision}, SourceRevision: revision, ExpectedResourceRevision: resourceRevision(row), Action: action, Definition: definition, Missing: missing}, nil
}
func (o *Owner) WithBindingLease(ctx context.Context, c hostoutbounds.Candidate, fn func(context.Context, hostoutbounds.Lease) error) (hostoutbounds.Finalization, error) {
	if ctx == nil || o.tx == nil {
		return hostoutbounds.TransactionPending, hostoutbounds.ErrLease
	}
	nested := false
	if _, ok := o.tx.Statement.ConnPool.(gorm.TxCommitter); ok {
		nested = true
	}
	err := o.tx.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := &Owner{tx: tx, source: o.source}
		return scoped.withLease(ctx, c, fn)
	})
	if err != nil {
		return hostoutbounds.TransactionUnknown, err
	}
	if nested {
		return hostoutbounds.TransactionPending, nil
	}
	return hostoutbounds.TransactionCommitted, nil
}
func (o *Owner) withLease(ctx context.Context, c hostoutbounds.Candidate, fn func(context.Context, hostoutbounds.Lease) error) error {
	if ctx == nil || o.tx == nil {
		return hostoutbounds.ErrLease
	}
	revision, err := o.sourceRevision(ctx)
	if err != nil {
		return err
	}
	row, owned, err := o.resource(ctx)
	if err != nil {
		return err
	}
	provider := o.provider(ctx)
	capability := hostcore.Unknown
	if coreprovider.SourceIdentityVerified(provider) {
		capability = hostcore.Available
		if c.Action == hostoutbounds.Upsert {
			var candidate model.Outbound
			if json.Unmarshal(c.Definition, &candidate) != nil {
				return hostoutbounds.ErrDefinition
			}
			fact := entitycapabilities.ResolveResource("outbounds", candidate.Type, candidate.Options)
			if !fact.Known {
				capability = hostcore.Unknown
			} else if !fact.Available {
				capability = hostcore.Unsupported
			}
		}
	}
	admitted := hostoutbounds.Admission{Binding: o.ref(), Resource: hostoutbounds.ResourceRef{Host: hostref.Local(), ID: row.Id, Tag: o.source.OutboundTag}, Provider: hostcore.Provider{ID: provider.ID, Revision: provider.Revision}, SourceRevision: revision, ResourceRevision: resourceRevision(row), Capability: capability, Owned: owned}
	lease := &bindingLease{owner: o, admitted: admitted}
	// The caller owns commit/rollback for the source rows and Outbounds together.
	// A successful callback proves staged persistence only.
	return fn(ctx, lease)
}

type bindingLease struct {
	owner    *Owner
	admitted hostoutbounds.Admission
	receipt  hostoutbounds.Observation
	wrote    bool
}

func (l *bindingLease) Admission() hostoutbounds.Admission { return l.admitted }
func (l *bindingLease) WriteBinding(ctx context.Context, c hostoutbounds.Candidate) error {
	if l.wrote {
		return hostoutbounds.ErrLease
	}
	l.wrote = true
	if err := ctx.Err(); err != nil {
		return err
	}
	// Recheck the exact before-image immediately before using the canonical writer.
	revision, err := l.owner.sourceRevision(ctx)
	if err != nil {
		return err
	}
	if revision != l.admitted.SourceRevision {
		return hostoutbounds.ErrRevision
	}
	row, owned, err := l.owner.resource(ctx)
	if err != nil {
		return err
	}
	if !owned {
		return hostoutbounds.ErrConflict
	}
	if resourceRevision(row) != l.admitted.ResourceRevision {
		return hostoutbounds.ErrRevision
	}
	tx := l.owner.tx.WithContext(ctx)
	switch c.Action {
	case hostoutbounds.Upsert:
		data, err := entityoutbounds.PrepareImportedCredentialDraft(c.Definition)
		if err != nil {
			return err
		}
		var fields map[string]any
		if json.Unmarshal(data, &fields) != nil {
			return hostoutbounds.ErrDefinition
		}
		fields["id"] = row.Id
		fields["remoteMissing"] = false
		fields["remoteMissingReason"] = ""
		fields["remoteMissingSince"] = 0
		fields["remoteMissingSource"] = ""
		data, err = json.Marshal(fields)
		if err != nil {
			return err
		}
		action := "new"
		if row.Id != 0 {
			action = "edit"
		}
		if _, err = entityoutbounds.Save(tx, action, data); err != nil {
			return err
		}
	case hostoutbounds.MarkMissing:
		if row.Id == 0 {
			return hostoutbounds.ErrConflict
		}
		if err := entityoutbounds.MarkSourceMissing(tx, row.Id, row.Tag, c.Missing); err != nil {
			return err
		}
	case hostoutbounds.Remove:
		if row.Id != 0 {
			if err := l.owner.validateDownloadReference(tx, row.Id); err != nil {
				return err
			}
			data, _ := json.Marshal(row.Tag)
			if _, err := entityoutbounds.Save(tx, "del", data); err != nil {
				return err
			}
		}
	default:
		return hostoutbounds.ErrDefinition
	}
	var observed model.Outbound
	err = tx.Where("tag = ?", c.Resource.Tag).First(&observed).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if c.Action == hostoutbounds.Remove {
		if observed.Id != 0 {
			return hostoutbounds.ErrObservation
		}
	} else {
		if observed.Id == 0 || observed.RemoteMissing != (c.Action == hostoutbounds.MarkMissing) {
			return hostoutbounds.ErrObservation
		}
		if c.Action == hostoutbounds.MarkMissing && (observed.RemoteMissingReason != c.Missing.Reason || observed.RemoteMissingSince != c.Missing.Since || observed.RemoteMissingSource != c.Missing.Source) {
			return hostoutbounds.ErrObservation
		}
	}
	resource := hostoutbounds.ResourceRef{Host: c.Resource.Host, ID: observed.Id, Tag: c.Resource.Tag}
	if c.Action == hostoutbounds.Remove {
		resource.ID = c.Resource.ID
	}
	l.receipt = hostoutbounds.Observation{Binding: c.Binding, Resource: resource, Provider: c.Provider, SourceRevision: c.SourceRevision, ResourceRevision: resourceRevision(observed), Present: observed.Id != 0, Missing: observed.RemoteMissing, CandidateDigest: hostoutbounds.Digest(c)}
	return nil
}
func (l *bindingLease) ObserveBinding(ctx context.Context) (hostoutbounds.Observation, error) {
	if err := ctx.Err(); err != nil {
		return hostoutbounds.Observation{}, err
	}
	if !l.wrote || l.receipt.CandidateDigest == "" {
		return hostoutbounds.Observation{}, hostoutbounds.ErrObservation
	}
	return l.receipt, nil
}
func (o *Owner) validateDownloadReference(tx *gorm.DB, id uint) error {
	if !tx.Migrator().HasTable("remote_outbound_subscriptions") {
		return nil
	}
	var n int64
	if err := tx.Table("remote_outbound_subscriptions").Where("download_outbound_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n != 0 {
		return errors.New("SUBSCRIPTION_DOWNLOAD_OUTBOUND_REFERENCED")
	}
	return nil
}

// ObserveBinding reads committed or caller-transaction state through the same
// exact source/resource identity. It establishes no Core runtime activation.
func (o *Owner) ObserveBinding(ctx context.Context, q hostoutbounds.Query) (hostoutbounds.Observation, error) {
	if q.Binding != o.ref() {
		return hostoutbounds.Observation{}, hostoutbounds.ErrScope
	}
	revision, err := o.sourceRevision(ctx)
	if err != nil {
		return hostoutbounds.Observation{}, err
	}
	row, _, err := o.resource(ctx)
	if err != nil {
		return hostoutbounds.Observation{}, err
	}
	if row.Tag != o.source.OutboundTag || o.source.OutboundId == nil || row.Id != *o.source.OutboundId {
		row = model.Outbound{}
	}
	resource := hostoutbounds.ResourceRef{Host: hostref.Local(), ID: row.Id, Tag: o.source.OutboundTag}
	if row.Id == 0 {
		resource.ID = q.Resource.ID
	}
	p := o.provider(ctx)
	if !coreprovider.SourceIdentityVerified(p) {
		return hostoutbounds.Observation{}, hostoutbounds.ErrProvider
	}
	return hostoutbounds.Observation{Binding: o.ref(), Resource: resource, Provider: hostcore.Provider{ID: p.ID, Revision: p.Revision}, SourceRevision: revision, ResourceRevision: resourceRevision(row), Present: row.Id != 0, Missing: row.RemoteMissing}, nil
}

// Retag keeps source identity, provider admission and the exact resource lease
// together while the canonical Outbounds owner performs the tag mutation.
func (o *Owner) Retag(ctx context.Context, oldTag, newTag string) (bool, error) {
	if o.tx == nil || ctx == nil {
		return false, hostoutbounds.ErrLease
	}
	if oldTag != o.source.OutboundTag {
		return false, hostoutbounds.ErrRevision
	}
	candidate, err := o.Prepare(ctx, hostoutbounds.MarkMissing, nil, hostoutbounds.Missing{})
	if err != nil {
		return false, err
	}
	changed := false
	_, err = o.WithBindingLease(ctx, candidate, func(ctx context.Context, lease hostoutbounds.Lease) error {
		admitted := lease.Admission()
		if !admitted.Owned {
			return hostoutbounds.ErrConflict
		}
		if admitted.SourceRevision != candidate.SourceRevision || admitted.ResourceRevision != candidate.ExpectedResourceRevision {
			return hostoutbounds.ErrRevision
		}
		if admitted.Capability != hostcore.Available {
			return hostoutbounds.ErrCapability
		}
		if admitted.Provider != candidate.Provider {
			return hostoutbounds.ErrProvider
		}
		if admitted.Resource.ID == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var writeErr error
		scoped, ok := lease.(*bindingLease)
		if !ok {
			return hostoutbounds.ErrLease
		}
		changed, writeErr = entityoutbounds.RetagSource(scoped.owner.tx.WithContext(ctx), admitted.Resource.ID, oldTag, newTag)
		return writeErr
	})
	return changed, err
}
