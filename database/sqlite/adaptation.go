package sqlite

import "github.com/MalenkiySolovey/solovey-ui/util/common"

// Adapt performs idempotent post-migration adjustments that
// ensure a database imported from an older S-UI version is fully usable on the
// current build:
//
//  1. Any remaining default admin/admin state is rotated to a random password
//     saved through the initial-admin.txt mechanism.
//  2. Other plaintext admin/user passwords are rehashed with bcrypt.
//  3. Indexes added by this fork are (re-)created if missing.
//  4. The `settings.version` row is updated to the current version so that
//     `database/migration` skips running again on the next startup.
//
// All steps are idempotent: running the function multiple times is safe.
//
// Adapt expects the package-level database to be open. It must be called after
// Init (so AutoMigrate already ran), but before the panel
// starts serving traffic.
func adapt() error {
	if db == nil {
		return common.NewError("sqlite.Adapt: database not initialized")
	}
	if err := ensureIndexes(); err != nil {
		return err
	}
	if err := rehashLegacyPasswords(db); err != nil {
		return err
	}
	if err := bumpVersionSetting(db); err != nil {
		return err
	}
	return nil
}
