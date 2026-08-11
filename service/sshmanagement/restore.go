package sshmanagement

import (
	"context"
	"time"

	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
)

func init() {
	dbhooks.RegisterImportPostOpenHook("core.ssh_management_recovery", func(ctx context.Context) error {
		// Restored active-looking state has no live artifact/challenge authority.
		// Mark it untrusted without invoking any provider or host mutation.
		return Shared().Repository.MarkRestoredUntrusted(ctx, time.Now().UTC())
	})
}
