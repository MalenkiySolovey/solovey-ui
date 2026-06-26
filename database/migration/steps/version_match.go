package steps

import (
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
)

func dbVersionMinorIs(version string, major int, minor int) bool {
	parsed, ok := versionpolicy.ParseSemver(versionpolicy.NormalizeVersion(version))
	if !ok {
		return false
	}
	return parsed.Major == major && parsed.Minor == minor
}
