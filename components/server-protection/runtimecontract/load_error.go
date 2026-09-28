package runtimecontract

import "errors"

type authorityLoadError struct {
	stage string
	cause error
}

func (e *authorityLoadError) Error() string { return "installed runtime root: " + e.stage }
func (e *authorityLoadError) Unwrap() error { return e.cause }

// AuthorityLoadFailureStage exposes only owner-defined classifications. The
// original cause remains available internally through errors.Is/errors.As.
func AuthorityLoadFailureStage(err error) string {
	var failure *authorityLoadError
	if errors.As(err, &failure) {
		return failure.stage
	}
	return "runtime_authority_load_failed"
}
