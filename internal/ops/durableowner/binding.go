package durableowner

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/durable"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/gorm"
)

// BackendAdapter is composition-only. Concrete handles never cross the public
// durable port. It reuses the caller's existing transaction/generation; it does
// not create a transaction manager, registry, journal or recovery engine.
type BackendAdapter struct {
	Migrate func(context.Context, *gorm.DB) error
	Restore func(context.Context, *gorm.DB) error
	Inspect func(context.Context, *gorm.DB, time.Time) durable.AuthorityStatus
	Drop    func(context.Context, *gorm.DB) error
}

type Binding struct {
	manifest manifest.Manifest
	owner    durable.Lifecycle
	backend  BackendAdapter
}

func Bind(item manifest.Manifest, owner durable.Lifecycle, backend BackendAdapter) (*Binding, error) {
	if err := item.Validate(); err != nil {
		return nil, err
	}
	if owner == nil || backend.Migrate == nil || backend.Restore == nil || backend.Inspect == nil || backend.Drop == nil || len(item.Database.Tables) == 0 {
		return nil, errors.New("typed durable owner capabilities are incomplete")
	}
	return &Binding{manifest: cloneManifest(item.Normalized()), owner: owner, backend: backend}, nil
}

func (b *Binding) Matches(item manifest.Manifest) bool {
	return b != nil && reflect.DeepEqual(b.manifest, item.Normalized())
}

func failure(outcome durable.Outcome, cause error) error {
	return &durable.Failure{Outcome: outcome, Cause: cause}
}

type capabilityKey struct{}

// An issued operation is bounded to one owner callback and one use. Its private
// context binds authority; child cancellation is honored without substituting
// the Backend transaction or maintenance context.
type operation struct {
	mu        sync.Mutex
	ctx       context.Context
	scope     durable.Scope
	db        *gorm.DB
	backend   BackendAdapter
	resources manifest.DurableResourceManifestV1
	used      bool
	completed bool
	expired   bool
	status    durable.AuthorityStatus
}

func (b *Binding) operation(ctx context.Context, db *gorm.DB, storage durable.Storage) (*operation, error) {
	if b == nil || ctx == nil || db == nil {
		return nil, failure(durable.Rejected, errors.New("durable storage binding is unavailable"))
	}
	if err := ctx.Err(); err != nil {
		return nil, failure(durable.Rejected, err)
	}
	op := &operation{db: db, backend: b.backend, resources: b.manifest.Database, scope: durable.Scope{Host: hostref.Local(), OwnerID: b.manifest.ID,
		Storage: storage, SchemaVersion: b.manifest.Database.SchemaVersion, MigrationVersion: b.manifest.Database.MigrationVersion}}
	if err := op.scope.Validate(); err != nil {
		return nil, failure(durable.Rejected, err)
	}
	op.ctx = context.WithValue(ctx, capabilityKey{}, op)
	return op, nil
}

func (o *operation) Scope() durable.Scope { return o.scope }

func (o *operation) run(ctx context.Context, mutate func(context.Context) error) (durable.Result, error) {
	rejected := durable.Result{Scope: o.scope, Outcome: durable.Rejected}
	if ctx == nil || ctx.Value(capabilityKey{}) != o {
		return rejected, failure(durable.Rejected, errors.New("durable capability context mismatch"))
	}
	if !o.mu.TryLock() {
		return rejected, failure(durable.Rejected, errors.New("durable capability is already in use"))
	}
	defer o.mu.Unlock()
	if o.expired || o.used {
		return rejected, failure(durable.Rejected, errors.New("durable capability is expired or consumed"))
	}
	if err := ctx.Err(); err != nil {
		return rejected, failure(durable.Rejected, err)
	}
	if err := o.ctx.Err(); err != nil {
		return rejected, failure(durable.Rejected, err)
	}
	// Preserve the Backend owner's authority and cancellation even if a feature
	// passes WithoutCancel or a child with different context values.
	bound, cancel := context.WithCancel(o.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	o.used = true
	if err := mutate(bound); err != nil {
		outcome := durable.Unknown
		if o.scope.Storage == durable.Candidate {
			outcome = durable.Rejected // Backend still owns disposal/exact abort.
		}
		return durable.Result{Scope: o.scope, Outcome: outcome}, failure(outcome, err)
	}
	if err := errors.Join(o.ctx.Err(), ctx.Err(), bound.Err()); err != nil {
		return durable.Result{Scope: o.scope, Outcome: durable.Unknown}, failure(durable.Unknown, err)
	}
	o.completed = true
	return durable.Result{Scope: o.scope, Outcome: durable.Verified}, nil
}

func (o *operation) finish(ownerErr error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expired = true
	if ownerErr != nil {
		var typed *durable.Failure
		if errors.As(ownerErr, &typed) {
			return ownerErr
		}
		outcome := durable.Rejected
		if o.used {
			outcome = durable.Unknown
		}
		return failure(outcome, ownerErr)
	}
	if !o.used || !o.completed {
		return failure(durable.Unknown, errors.New("durable owner did not verify its admitted operation"))
	}
	return nil
}

func (o *operation) expire() { o.mu.Lock(); o.expired = true; o.mu.Unlock() }

type migrationPort struct{ *operation }

func (p migrationPort) MigrateAndValidate(ctx context.Context) (durable.Result, error) {
	return p.run(ctx, func(ctx context.Context) error {
		db := p.db.WithContext(ctx)
		if err := p.backend.Migrate(ctx, db); err != nil {
			return err
		}
		return validateDeclaredResources(ctx, db, p.resources)
	})
}

type restorePort struct{ *operation }

func (p restorePort) NormalizeRestore(ctx context.Context) (durable.Result, error) {
	return p.run(ctx, func(ctx context.Context) error {
		db := p.db.WithContext(ctx)
		if err := p.backend.Restore(ctx, db); err != nil {
			return err
		}
		return validateDeclaredResources(ctx, db, p.resources)
	})
}

type inspectionPort struct{ *operation }

func (p inspectionPort) InspectDropAuthority(ctx context.Context, now time.Time) (durable.AuthorityStatus, error) {
	_, err := p.run(ctx, func(ctx context.Context) error {
		p.status = p.backend.Inspect(ctx, p.db.WithContext(ctx), now)
		if p.status.State != "VERIFIED_SAFE" && p.status.State != "BLOCKED" && p.status.State != "UNAVAILABLE" {
			return errors.New("durable owner authority result is invalid")
		}
		p.status.ReasonCodes = append([]string(nil), p.status.ReasonCodes...)
		return nil
	})
	if err != nil {
		return durable.AuthorityStatus{}, err
	}
	return durable.AuthorityStatus{State: p.status.State, ReasonCodes: append([]string(nil), p.status.ReasonCodes...)}, err
}

type dropPort struct{ *operation }

func (p dropPort) DropOwnedData(ctx context.Context) (durable.Result, error) {
	return p.run(ctx, func(ctx context.Context) error {
		if err := verifyDropAdmission(ctx, p.db, p.scope.OwnerID); err != nil {
			return err
		}
		return p.backend.Drop(ctx, p.db.WithContext(ctx))
	})
}

func (b *Binding) Migrate(ctx context.Context) error {
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()
	op, err := b.operation(ctx, dbsqlite.DB(), durable.Live)
	if err != nil {
		return err
	}
	defer op.expire()
	return op.finish(b.owner.Migrate(op.ctx, migrationPort{op}))
}

// ValidateLive checks an already APPLIED schema without silently rerunning a
// migration or normalizing publication data.
func (b *Binding) ValidateLive(ctx context.Context) error {
	if b == nil || ctx == nil || dbsqlite.DB() == nil {
		return errors.New("durable live validation is unavailable")
	}
	return validateDeclaredResources(ctx, dbsqlite.DB().WithContext(ctx), b.manifest.Database)
}

// RunCandidate binds only the supplied private storage. The existing SQLite
// operation lease keeps the live generation stable during identity checks.
func (b *Binding) RunCandidate(ctx context.Context, db *gorm.DB) error {
	if db == nil || ctx == nil {
		return errors.New("durable restore candidate is unavailable")
	}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := rejectLiveCandidate(ctx, db); err != nil {
		return failure(durable.Rejected, err)
	}
	migrate, err := b.operation(ctx, db, durable.Candidate)
	if err != nil {
		return err
	}
	defer migrate.expire()
	if err := migrate.finish(b.owner.Migrate(migrate.ctx, migrationPort{migrate})); err != nil {
		return err
	}
	restore, err := b.operation(ctx, db, durable.Candidate)
	if err != nil {
		return err
	}
	defer restore.expire()
	return restore.finish(b.owner.RehearseRestore(restore.ctx, restorePort{restore}))
}

// A second SQL pool, alternate path or hard link to the live SQLite file is
// still live storage. Pool inequality alone is not evidence of isolation.
func rejectLiveCandidate(ctx context.Context, candidate *gorm.DB) error {
	if dbsqlite.IsPrivateDatabase(ctx, candidate) || !dbsqlite.IsOpen() {
		return nil
	}
	live := dbsqlite.DB()
	pool, err := candidate.DB()
	if err != nil {
		return err
	}
	livePool, err := live.DB()
	if err != nil {
		return err
	}
	if pool == livePool {
		return errors.New("live storage cannot be used as a restore candidate")
	}
	identity := func(db *gorm.DB) (os.FileInfo, error) {
		var databases []struct{ Name, File string }
		if err := db.WithContext(ctx).Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
			return nil, err
		}
		for _, database := range databases {
			if database.Name == "main" && database.File != "" {
				return os.Stat(database.File)
			}
		}
		return nil, errors.New("durable database file identity is unavailable")
	}
	candidateFile, err := identity(candidate)
	if err != nil {
		return err
	}
	liveFile, err := identity(live)
	if err != nil {
		return err
	}
	if os.SameFile(candidateFile, liveFile) {
		return errors.New("live storage cannot be used as a restore candidate")
	}
	return nil
}

func (b *Binding) Inspect(ctx context.Context, db *gorm.DB, now time.Time) (durable.AuthorityStatus, error) {
	op, err := b.operation(ctx, db, durable.Live)
	if err != nil {
		return durable.AuthorityStatus{}, err
	}
	defer op.expire()
	status, err := b.owner.InspectDropAuthority(op.ctx, inspectionPort{op}, now)
	if err = op.finish(err); err != nil {
		return durable.AuthorityStatus{}, err
	}
	if !reflect.DeepEqual(status, op.status) {
		return durable.AuthorityStatus{}, errors.New("durable authority evidence changed")
	}
	return status, nil
}

func (b *Binding) DropData(ctx context.Context) error {
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()
	db := dbsqlite.DB()
	if err := verifyDropAdmission(ctx, db, b.manifest.ID); err != nil {
		return failure(durable.Rejected, err)
	}
	op, err := b.operation(ctx, db, durable.Live)
	if err != nil {
		return err
	}
	defer op.expire()
	return op.finish(b.owner.DropData(op.ctx, dropPort{op}))
}

type dropAdmissionKey struct{}
type dropAdmission struct {
	owner, operation string
	revision         uint64
	pool             *sql.DB
}

// ContextForDrop is called only by Backend DataLifecycle after its durable
// DROPPING transition. The row/revision and generation are rechecked on use;
// possession of an identifier or a preview is never cleanup authority.
func ContextForDrop(ctx context.Context, db *gorm.DB, owner, operation string, revision uint64) (context.Context, error) {
	if ctx == nil || db == nil {
		return nil, errors.New("data drop admission is unavailable")
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	bound := context.WithValue(ctx, dropAdmissionKey{}, dropAdmission{owner: owner, operation: operation, revision: revision, pool: pool})
	if err := verifyDropAdmission(bound, db, owner); err != nil {
		return nil, err
	}
	return bound, nil
}

func verifyDropAdmission(ctx context.Context, db *gorm.DB, owner string) error {
	if ctx == nil || db == nil {
		return errors.New("data drop admission is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	admission, ok := ctx.Value(dropAdmissionKey{}).(dropAdmission)
	if !ok || admission.owner != owner || admission.revision == 0 {
		return errors.New("data drop requires the admitted owner operation")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	if pool != admission.pool {
		return errors.New("data drop storage generation changed")
	}
	var row model.DataLifecycleOperation
	if err := db.WithContext(ctx).Where("operation_id = ? AND owner_id = ? AND revision = ? AND kind = ? AND state = ? AND restored_untrusted = ?",
		admission.operation, owner, admission.revision, "DROP_DATA", "DROPPING", false).First(&row).Error; err != nil {
		return err
	}
	if bytes, err := hex.DecodeString(row.BackupRef); err != nil || len(bytes) != 32 {
		return errors.New("data drop recovery backup is unverified")
	}
	return nil
}
