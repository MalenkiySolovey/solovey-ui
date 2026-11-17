//go:build !linux

package datalifecycle

// Windows does not expose the POSIX directory-fsync primitive. Publication
// still performs the same explicit boundary so tests can inject it and Linux
// builds provide the platform primitive.
func syncRecoveryDirectory(string) error { return nil }
