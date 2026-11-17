package web

import (
	owner "github.com/MalenkiySolovey/solovey-backend/http/frontend"
	"io/fs"
)

func validateFrontend(bundle fs.FS) error { return owner.Validate(bundle) }
