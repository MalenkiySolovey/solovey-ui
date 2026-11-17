package web

import (
	owner "github.com/MalenkiySolovey/solovey-ui/backend/http/frontend"
	"io/fs"
)

func validateFrontend(bundle fs.FS) error { return owner.Validate(bundle) }
