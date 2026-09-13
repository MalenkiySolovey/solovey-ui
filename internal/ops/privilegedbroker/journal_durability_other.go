//go:build !linux

package privilegedbroker

// Privileged journal production is Linux-only. Other targets retain the same
// serialized state machine for portable regression tests; their host
// filesystems do not expose the Linux directory-fsync contract.
func syncJournalDirectory(string) error { return nil }
