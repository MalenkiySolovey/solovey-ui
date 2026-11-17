package sqlite

import canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/bootstrap"

type UnsupportedVersionError = canonical.UnsupportedVersionError

func preflightSupportedVersion(path string) error {
	return storageInitializer().PreflightSupportedVersion(path)
}
func ValidateSupportedVersionFile(path string) error { return preflightSupportedVersion(path) }
