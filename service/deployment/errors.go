package deployment

import "errors"

var (
	ErrProviderUnavailable = errors.New("deployment provider unavailable")
	ErrRevisionMismatch    = errors.New("deployment revision mismatch")
	ErrOperationConflict   = errors.New("deployment operation conflict")
	ErrUnsafeMigration     = errors.New("deployment migration is unsafe")
)
