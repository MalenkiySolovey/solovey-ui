package openwrt

import "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt/persistencepolicy"

// ApprovedPersistentFilesystem projects the canonical OpenWrt backing policy.
func ApprovedPersistentFilesystem(value string) bool {
	return persistencepolicy.ApprovedPersistentFilesystem(value)
}

// ApprovedPersistentFilesystemNames returns the canonical defensive policy copy.
func ApprovedPersistentFilesystemNames() []string {
	return persistencepolicy.ApprovedPersistentFilesystemNames()
}
