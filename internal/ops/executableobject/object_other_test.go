//go:build !linux

package executableobject

import (
	"path/filepath"
	"testing"
)

func TestOpenOtherPlatformIsUnavailable(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "candidate"), Policy{}); err == nil {
		t.Fatal("non-Linux executable-object authority unexpectedly available")
	}
}
