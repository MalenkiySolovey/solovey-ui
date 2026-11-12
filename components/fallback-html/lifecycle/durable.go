//go:build !minimal

// Package lifecycle contains Fallback's storage-independent durable hooks.
// Concrete schema, file and legacy adapters are bound by the Backend owner.
package lifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/durable"
)

type Owner struct{}

func validate(scope durable.Scope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.OwnerID != "fallback-html" || scope.SchemaVersion != "1" || scope.MigrationVersion != "1" {
		return errors.New("fallback-html durable owner identity mismatch")
	}
	return nil
}

func (Owner) Migrate(ctx context.Context, port durable.Migration) error {
	if err := validate(port.Scope()); err != nil {
		return err
	}
	_, err := port.MigrateAndValidate(ctx)
	return err
}

func (Owner) RehearseRestore(ctx context.Context, port durable.Restore) error {
	if err := validate(port.Scope()); err != nil {
		return err
	}
	if port.Scope().Storage != durable.Candidate {
		return errors.New("fallback-html restore requires candidate storage")
	}
	_, err := port.NormalizeRestore(ctx)
	return err
}

func (Owner) InspectDropAuthority(ctx context.Context, port durable.Inspection, now time.Time) (durable.AuthorityStatus, error) {
	if err := validate(port.Scope()); err != nil {
		return durable.AuthorityStatus{}, err
	}
	return port.InspectDropAuthority(ctx, now)
}

func (Owner) DropData(ctx context.Context, port durable.DataDrop) error {
	if err := validate(port.Scope()); err != nil {
		return err
	}
	if port.Scope().Storage != durable.Live {
		return errors.New("fallback-html data drop requires live admission")
	}
	_, err := port.DropOwnedData(ctx)
	return err
}
