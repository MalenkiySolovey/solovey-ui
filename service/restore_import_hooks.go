package service

import (
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
)

func init() {
	dbhooks.RegisterImportPostOpenHook("service.restore_post_open", runRestoreImportServicePostOpenActions)
}
