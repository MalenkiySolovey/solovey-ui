//go:build linux

package artifacts

import (
	"errors"
	"os"
)

func syncArtifactDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
