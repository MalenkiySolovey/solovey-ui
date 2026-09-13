//go:build linux

package privilegedbroker

import (
	"errors"
	"os"
)

func syncJournalDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
