//go:build linux

package backup

import "os"

// SyncRecoveryDirectory persists directory entries after recovery file publication.
func SyncRecoveryDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
