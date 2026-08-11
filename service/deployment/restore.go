package deployment

import (
	"context"

	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
)

func init() {
	dbhooks.RegisterImportPostOpenHook("core.deployment_recovery", func(ctx context.Context) error {
		// Imported metadata cannot confer live broker checkpoint or activation
		// authority. Reconciliation starts again from a fresh provider doctor.
		return Shared().Repository.MarkRestoredUntrusted(ctx)
	})
}
