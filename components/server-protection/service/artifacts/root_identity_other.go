//go:build !linux

package artifacts

import "os"

func validateStorageRootOwnership(string, os.FileMode) error { return nil }
