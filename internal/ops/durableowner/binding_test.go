package durableowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/durable"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type capabilityOwner struct {
	migrate func(context.Context, durable.Migration) error
	inspect func(durable.AuthorityStatus) durable.AuthorityStatus
	port    durable.Migration
	ctx     context.Context
}

func (o *capabilityOwner) Migrate(ctx context.Context, p durable.Migration) error {
	o.port, o.ctx = p, ctx
	if o.migrate != nil {
		return o.migrate(ctx, p)
	}
	_, err := p.MigrateAndValidate(ctx)
	return err
}
func (*capabilityOwner) RehearseRestore(ctx context.Context, p durable.Restore) error {
	_, err := p.NormalizeRestore(ctx)
	return err
}
func (o *capabilityOwner) InspectDropAuthority(ctx context.Context, p durable.Inspection, now time.Time) (durable.AuthorityStatus, error) {
	status, err := p.InspectDropAuthority(ctx, now)
	if o.inspect != nil {
		status = o.inspect(status)
	}
	return status, err
}
func (*capabilityOwner) DropData(ctx context.Context, p durable.DataDrop) error {
	_, err := p.DropOwnedData(ctx)
	return err
}

func bindingFixture(t *testing.T, owner *capabilityOwner, before func()) (*Binding, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "candidate.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	item := manifest.Manifest{ID: "typed-owner", Name: "Typed owner", Version: "1", Delivery: "in-process", Database: manifest.Database{Tables: []string{"typed_owner_rows"}}}
	binding, err := Bind(item, owner, BackendAdapter{
		Migrate: func(ctx context.Context, db *gorm.DB) error {
			if before != nil {
				before()
			}
			if err := db.WithContext(ctx).Exec("CREATE TABLE typed_owner_rows (value TEXT)").Error; err != nil {
				return err
			}
			return db.WithContext(ctx).Exec("INSERT INTO typed_owner_rows VALUES ('migrated')").Error
		},
		Restore: func(ctx context.Context, db *gorm.DB) error {
			return db.WithContext(ctx).Exec("UPDATE typed_owner_rows SET value='normalized'").Error
		},
		Inspect: func(context.Context, *gorm.DB, time.Time) durable.AuthorityStatus {
			return durable.AuthorityStatus{State: "BLOCKED", ReasonCodes: []string{"guarded"}}
		},
		Drop: func(context.Context, *gorm.DB) error { t.Fatal("candidate acquired data-drop capability"); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return binding, db
}

func TestTypedBindingRejectsSkippedAndSwallowedOperations(t *testing.T) {
	for _, variant := range []string{"skip", "wrong_context", "swallowed_error"} {
		t.Run(variant, func(t *testing.T) {
			owner := &capabilityOwner{}
			binding, db := bindingFixture(t, owner, nil)
			owner.migrate = func(ctx context.Context, p durable.Migration) error {
				switch variant {
				case "skip":
					return nil
				case "wrong_context":
					_, _ = p.MigrateAndValidate(context.Background())
					return nil
				default:
					_, _ = p.MigrateAndValidate(ctx)
					return nil
				}
			}
			if variant == "swallowed_error" {
				binding.backend.Migrate = func(context.Context, *gorm.DB) error { return errors.New("storage unavailable") }
			}
			err := binding.RunCandidate(t.Context(), db)
			var failure *durable.Failure
			if !errors.As(err, &failure) || failure.Outcome != durable.Unknown {
				t.Fatalf("false completion: %v", err)
			}
			if db.Migrator().HasTable("typed_owner_rows") {
				t.Fatal("rejected capability changed candidate")
			}
		})
	}
}

func TestTypedBindingFencesConcurrentRepeatedAndExpiredCapability(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	owner := &capabilityOwner{}
	binding, db := bindingFixture(t, owner, func() { close(entered); <-release })
	owner.migrate = func(ctx context.Context, p durable.Migration) error {
		result := make(chan error, 1)
		go func() { _, err := p.MigrateAndValidate(ctx); result <- err }()
		<-entered
		if _, err := p.MigrateAndValidate(ctx); err == nil {
			t.Error("concurrent capability reuse accepted")
		}
		close(release)
		return <-result
	}
	if err := binding.RunCandidate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.port.MigrateAndValidate(owner.ctx); err == nil {
		t.Fatal("retained capability survived owner callback")
	}
	var values []string
	if err := db.Raw("SELECT value FROM typed_owner_rows").Scan(&values).Error; err != nil || len(values) != 1 || values[0] != "normalized" {
		t.Fatalf("candidate operation duplicated or lost: %v %v", values, err)
	}
}

func TestTypedBindingRejectsForgedAuthorityEvidence(t *testing.T) {
	owner := &capabilityOwner{inspect: func(durable.AuthorityStatus) durable.AuthorityStatus {
		return durable.AuthorityStatus{State: "VERIFIED_SAFE"}
	}}
	binding, db := bindingFixture(t, owner, nil)
	if _, err := binding.Inspect(t.Context(), db, time.Now()); err == nil {
		t.Fatal("owner replaced a guarding result with safe authority")
	}
}

func TestTypedBindingRejectsIncompleteBackendCapabilities(t *testing.T) {
	item := manifest.Manifest{ID: "incomplete-owner", Name: "Incomplete owner", Version: "1", Delivery: "in-process", Database: manifest.Database{Tables: []string{"incomplete_owner_rows"}}}
	if _, err := Bind(item, &capabilityOwner{}, BackendAdapter{}); err == nil {
		t.Fatal("missing capabilities admitted")
	}
}

func TestTypedBindingPreservesBackendCancellation(t *testing.T) {
	owner := &capabilityOwner{}
	binding, db := bindingFixture(t, owner, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend := binding.backend.Migrate
	binding.backend.Migrate = func(ctx context.Context, db *gorm.DB) error {
		if err := backend(ctx, db); err != nil {
			return err
		}
		cancel()
		return nil
	}
	owner.migrate = func(ctx context.Context, p durable.Migration) error {
		_, err := p.MigrateAndValidate(context.WithoutCancel(ctx))
		return err
	}
	if err := binding.RunCandidate(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("feature bypassed Backend cancellation: %v", err)
	}
}
