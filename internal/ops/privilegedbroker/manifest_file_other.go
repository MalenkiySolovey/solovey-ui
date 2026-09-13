//go:build !linux

package privilegedbroker

import (
	"errors"
	"os"
)

func readTrustedManifest(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 256<<10 {
		return nil, errors.New("broker client manifest is unsafe")
	}
	return os.ReadFile(path)
}
