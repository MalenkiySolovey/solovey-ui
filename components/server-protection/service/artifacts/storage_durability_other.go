//go:build !linux

package artifacts

// Windows and the portable fake gate cannot fsync directories. The Linux
// production implementation supplies the durability barrier; publication
// fault tests inject the same boundary on every platform.
func syncArtifactDirectory(string) error { return nil }
