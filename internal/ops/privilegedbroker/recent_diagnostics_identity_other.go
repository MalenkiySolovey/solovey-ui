//go:build !linux

package privilegedbroker

import "os"

func diagnosticCurrentUID() (uint32, bool) {
	return 0, false
}

func diagnosticFileUIDFromInfo(os.FileInfo) (uint32, bool) {
	return 0, false
}
