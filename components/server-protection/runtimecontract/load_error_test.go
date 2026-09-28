package runtimecontract

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAuthorityLoadFailureKeepsCausePrivate(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "sensitive-fixture-value", Err: os.ErrPermission}
	err := &authorityLoadError{stage: "runtime_root_load_failed", cause: cause}
	var pathError *os.PathError
	if AuthorityLoadFailureStage(err) != "runtime_root_load_failed" || strings.Contains(err.Error(), cause.Path) ||
		!errors.Is(err, os.ErrPermission) || !errors.As(err, &pathError) || pathError != cause {
		t.Fatal("bounded diagnostic lost classification, exposed cause, or broke internal chaining")
	}
}
