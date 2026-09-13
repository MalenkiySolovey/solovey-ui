package deployment

import "errors"

var (
	ErrProviderUnavailable = errors.New("deployment provider unavailable")
	ErrRevisionMismatch    = errors.New("deployment revision mismatch")
	ErrOperationConflict   = errors.New("deployment operation conflict")
	ErrUnsafeMigration     = errors.New("deployment migration is unsafe")
	ErrPackageManaged      = errors.New("deployment lifecycle is owned by the package manager")
	ErrPackagePosture      = errors.New("package-managed deployment posture is unavailable")
	ErrStatePersistence    = errors.New("deployment state persistence is unavailable")
)

type definitiveCheckpointReleaseError struct{ err error }

func (e definitiveCheckpointReleaseError) Error() string { return e.err.Error() }
func (e definitiveCheckpointReleaseError) Unwrap() error { return e.err }

func definitiveCheckpointReleaseFailure(err error) error {
	if err == nil {
		return nil
	}
	return definitiveCheckpointReleaseError{err: err}
}

func isDefinitiveCheckpointReleaseFailure(err error) bool {
	var target definitiveCheckpointReleaseError
	return errors.As(err, &target)
}
