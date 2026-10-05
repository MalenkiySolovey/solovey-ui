//go:build !minimal && with_utls

package importxui

import "testing"

func TestCapabilityImportRealityHonorsOfficialProfile(t *testing.T) {
	assertRealityProfileImport(t, true)
}
