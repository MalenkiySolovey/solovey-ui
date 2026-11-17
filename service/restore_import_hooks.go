package service

import (
	dbhooks "github.com/MalenkiySolovey/solovey-ui/backend/storage/hooks"
)

func init() {
	dbhooks.RegisterImportPostOpenHook("service.restore_post_open", runRestoreImportServicePostOpenActions)
}
