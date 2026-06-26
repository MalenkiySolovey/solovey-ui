package backup

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type importPostAction struct {
	stage           string
	rollbackOnError bool
	run             func(context.Context) error
}

type importRollbackFunc func(stage string, cause error) error

func importRollbackProtectedPostActions(dbPath string) []importPostAction {
	return []importPostAction{
		{
			stage:           "migrating imported db",
			rollbackOnError: true,
			run: func(context.Context) error {
				return migration.MigrateDb()
			},
		},
		{
			stage:           "opening imported db",
			rollbackOnError: true,
			run: func(context.Context) error {
				return dbsqlite.Init(dbPath)
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

func importFinalPostActions() []importPostAction {
	return []importPostAction{
		{
			stage: "restarting app",
			run: func(context.Context) error {
				// main.go traps SIGHUP and re-runs app.Init -> Start, where
				// migration is a no-op against the now-current imported DB.
				return SendSighup()
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
	return common.NewErrorf("Error %s: %v", action.stage, cause)
}
