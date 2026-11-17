package backup

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/hooks"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

type importPostAction struct {
	stage           string
	rollbackOnError bool
	run             func(context.Context) error
}

type importRollbackFunc func(stage string, cause error) error

func (r Restorer) importRollbackProtectedPostActions(dbPath string, owners []RestoreOwnerStatus, files ...*FileBackupManifest) []importPostAction {
	return []importPostAction{
		{stage: "migrating staged owner state", rollbackOnError: true, run: func(ctx context.Context) error {
			_, err := r.MigrateOwners(ctx, dbPath, nil, files...)
			return err
		}},
		{
			stage:           "opening imported db",
			rollbackOnError: true,
			run: func(ctx context.Context) error {
				return r.OpenDatabase(ctx, dbPath)
			},
		},
		{stage: "restoring logical owner files", rollbackOnError: true, run: func(ctx context.Context) error {
			var m *FileBackupManifest
			if len(files) == 1 {
				m = files[0]
			}
			return r.RestoreOwnerFiles(ctx, dbsqlite.DB(), m, true)
		}},
		{
			stage:           "normalizing installed restore owners",
			rollbackOnError: true,
			run: func(ctx context.Context) error {
				_, err := r.NormalizeOwners(ctx, dbsqlite.DB(), owners)
				return err
			},
		},
		{
			stage:           "running restore post-open actions",
			rollbackOnError: true,
			run: func(ctx context.Context) error {
				return hooks.RunPostOpen(ctx)
			},
		},
		{
			stage:           "resetting in-memory caches",
			rollbackOnError: true,
			run: func(ctx context.Context) error {
				return hooks.ResetCaches(ctx)
			},
		},
	}
}

func (r Restorer) importFinalPostActions() []importPostAction {
	return []importPostAction{
		{
			stage: "restarting app",
			run: func(context.Context) error {
				// Mandatory database owner rebinding has already completed in
				// the protected cache-reset phase before candidate acceptance.
				return r.Restart()
			},
		},
	}
}

func runImportPostActions(ctx context.Context, actions []importPostAction, rollback importRollbackFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, action := range actions {
		if err := ctx.Err(); err != nil {
			return handleImportPostActionError(action, rollback, err)
		}
		if action.run == nil {
			continue
		}
		if err := action.run(ctx); err != nil {
			return handleImportPostActionError(action, rollback, err)
		}
	}
	return ctx.Err()
}

func handleImportPostActionError(action importPostAction, rollback importRollbackFunc, cause error) error {
	if action.rollbackOnError && rollback != nil {
		return rollback(action.stage, cause)
	}
	return fmt.Errorf("Error %s: %v", action.stage, cause)
}
