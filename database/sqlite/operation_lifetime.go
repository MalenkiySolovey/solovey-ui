package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"time"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	ErrMaintenance   = errors.New("database maintenance is in progress")
	ErrRetired       = errors.New("database operation generation is unavailable")
	activeGeneration *generation
	maintenance      *Maintenance
	lifetimeChanged  = make(chan struct{})
)

const maintenanceDrainTimeout = 30 * time.Second

type generationPhase uint8

const (
	generationActive generationPhase = iota
	generationDraining
	generationPrivate
	generationRetired
)

// All generation and lease fields are guarded by the existing dbMu. The
// database/sql pool owns Rows, Tx, Conn and Raw callback resources until their
// driver connection can actually close; there is no second resource manager.
type generation struct {
	pool         *sql.DB
	cfg          dbPoolConfig
	phase        generationPhase
	initializing bool
	connections  int
	operations   int
	closeErr     error
}

type operationKey struct{}
type maintenanceKey struct{}
type operationLease struct {
	generation *generation
	released   bool
	suspended  bool
}

// Maintenance is an expiring, owner-local capability. Its context is never
// placed on the public GORM handle or a cached ordinary repository.
type Maintenance struct {
	endOnce sync.Once
	request *operationLease
	gap     *gorm.DB
	gapPool *sql.DB
	expired bool
}

func changedLocked() {
	close(lifetimeChanged)
	lifetimeChanged = make(chan struct{})
}

func operationFrom(ctx context.Context) *operationLease {
	if ctx == nil {
		return nil
	}
	lease, _ := ctx.Value(operationKey{}).(*operationLease)
	return lease
}
func maintenanceFrom(ctx context.Context) *Maintenance {
	if ctx == nil {
		return nil
	}
	lease, _ := ctx.Value(maintenanceKey{}).(*Maintenance)
	return lease
}

// AcquireOperation spans a finite semantic operation, including gaps between
// SQL calls and external I/O. Nested context admissions reuse the outer lease.
// Database-free startup/test jobs do not acquire authority over a future DB.
func AcquireOperation(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("database operation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	if owner := maintenanceFrom(ctx); owner != nil {
		if owner != maintenance || owner.expired {
			return nil, nil, ErrRetired
		}
		return ctx, func() {}, nil
	}
	if lease := operationFrom(ctx); lease != nil {
		if lease.released {
			return nil, nil, ErrRetired
		}
		if lease.suspended {
			return nil, nil, ErrMaintenance
		}
		if lease.generation != activeGeneration || lease.generation.phase == generationRetired {
			return nil, nil, ErrRetired
		}
		return ctx, func() {}, nil
	}
	if maintenance != nil {
		return nil, nil, ErrMaintenance
	}
	if activeGeneration == nil {
		return ctx, func() {}, nil
	}
	if activeGeneration.phase != generationActive {
		return nil, nil, ErrMaintenance
	}
	lease := &operationLease{generation: activeGeneration}
	lease.generation.operations++
	changedLocked()
	return context.WithValue(ctx, operationKey{}, lease), func() { releaseOperation(lease) }, nil
}

func releaseOperation(lease *operationLease) {
	if lease == nil {
		return
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	if lease.released {
		return
	}
	lease.released = true
	if !lease.suspended {
		lease.generation.operations--
	}
	changedLocked()
}

// ReleaseOperation ends the finite WebSocket handshake admission before the
// unbounded stream. Existing stream session validation remains its authority.
func ReleaseOperation(ctx context.Context) { releaseOperation(operationFrom(ctx)) }

func BeginMaintenance(ctx context.Context) (*Maintenance, error) {
	if ctx == nil {
		return nil, errors.New("database maintenance context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dbMu.Lock()
	if owner := maintenanceFrom(ctx); owner != nil && (owner != maintenance || owner.expired) {
		dbMu.Unlock()
		return nil, ErrRetired
	}
	if maintenance != nil {
		dbMu.Unlock()
		return nil, ErrMaintenance
	}
	if db == nil || activeGeneration == nil || activeGeneration.phase != generationActive {
		dbMu.Unlock()
		return nil, ErrRetired
	}
	request := operationFrom(ctx)
	if request != nil && (request.released || request.suspended || request.generation != activeGeneration) {
		dbMu.Unlock()
		return nil, ErrRetired
	}
	gapPool := sql.OpenDB(unavailableConnector{})
	gap := db.Session(&gorm.Session{NewDB: true, Context: context.Background(), Logger: gormlogger.Discard})
	gap.Config.ConnPool, gap.Statement.ConnPool = gapPool, gapPool
	gap.Config.Dialector = gormsqlite.New(gormsqlite.Config{Conn: gapPool})
	gap.Error = ErrMaintenance
	owner := &Maintenance{request: request, gap: gap, gapPool: gapPool}
	maintenance = owner
	if request != nil {
		request.suspended = true
		request.generation.operations--
	}
	current := activeGeneration
	changedLocked()
	dbMu.Unlock()
	drainCtx, cancel := context.WithTimeout(ctx, maintenanceDrainTimeout)
	defer cancel()
	if err := waitGeneration(drainCtx, current, false); err != nil {
		owner.End()
		return nil, err
	}
	return owner, nil
}

func (m *Maintenance) Context(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, maintenanceKey{}, m)
}

func (m *Maintenance) Active() bool {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return m != nil && maintenance == m && !m.expired
}

// End publishes only the currently opened/rebound generation. The restore
// lifecycle calls it after durable acceptance or successful exact recovery.
func (m *Maintenance) End() {
	if m == nil {
		return
	}
	m.endOnce.Do(func() {
		dbMu.RLock()
		if maintenance != m || m.expired {
			dbMu.RUnlock()
			return
		}
		publish := activeGeneration
		dbMu.RUnlock()
		// Pool setters may close driver connections; never call them under
		// dbMu. Admissions remain frozen until configuration is restored.
		if publish != nil {
			applyDBPoolConfig(publish.pool, publish.cfg)
		}
		dbMu.Lock()
		if maintenance != m || m.expired {
			dbMu.Unlock()
			return
		}
		m.expired = true
		maintenance = nil
		if activeGeneration != nil && activeGeneration.phase == generationPrivate {
			activeGeneration.phase = generationActive
		}
		if request := m.request; request != nil && !request.released {
			request.suspended = false
			if activeGeneration != nil && activeGeneration.phase == generationActive {
				request.generation = activeGeneration
				request.generation.operations++
			} else {
				request.released = true
			}
		}
		changedLocked()
		dbMu.Unlock()
		_ = m.gapPool.Close()
	})
}

func IsMaintenanceContext(ctx context.Context) bool {
	dbMu.RLock()
	defer dbMu.RUnlock()
	owner := maintenanceFrom(ctx)
	return owner != nil && maintenance == owner && !owner.expired
}

func IsOpen() bool {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return db != nil
}

func waitGeneration(ctx context.Context, current *generation, connections bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		dbMu.RLock()
		count := current.operations
		if connections {
			count = current.connections
		}
		err, changed := current.closeErr, lifetimeChanged
		dbMu.RUnlock()
		if connections && err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (g *generation) permittedLocked(ctx context.Context) error {
	if g.phase == generationRetired {
		return ErrRetired
	}
	if g.initializing {
		return nil
	} // GORM's constructor queries use Background.
	if owner := maintenanceFrom(ctx); owner != nil {
		if owner != maintenance || owner.expired {
			return ErrRetired
		}
		return nil
	}
	if lease := operationFrom(ctx); lease != nil {
		if lease.released || lease.generation != g {
			return ErrRetired
		}
		if lease.suspended {
			return ErrMaintenance
		}
	}
	if g.phase != generationActive {
		return ErrMaintenance
	}
	return nil
}
func (g *generation) permitted(ctx context.Context) error {
	if ctx == nil {
		return errors.New("database SQL context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dbMu.RLock()
	defer dbMu.RUnlock()
	return g.permittedLocked(ctx)
}

type unavailableConnector struct{}

func (unavailableConnector) Driver() driver.Driver                        { return unavailableDriver{} }
func (unavailableConnector) Connect(context.Context) (driver.Conn, error) { return nil, ErrMaintenance }

type unavailableDriver struct{}

func (unavailableDriver) Open(string) (driver.Conn, error) { return nil, ErrMaintenance }
