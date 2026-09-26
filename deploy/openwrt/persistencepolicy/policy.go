package persistencepolicy

import "strings"

// ApprovedPersistentFilesystems is the single OpenWrt rootfs-data filesystem
// policy shared by durability proofing and installed-runtime projections.
// Keep this list limited to filesystems that the locked OpenWrt platform can
// use for persistent writable state; volatile filesystems are classified by
// the caller and are never included here.
var approvedPersistentFilesystems = [...]string{
	"ext2", "ext3", "ext4", "f2fs", "ubifs", "jffs2", "btrfs", "xfs",
}

// ApprovedPersistentFilesystem reports whether value is one of the exact
// approved OpenWrt persistent backing filesystems. Filesystem names are
// case-insensitive because kernel mount evidence is normalized at the
// projection boundary.
func ApprovedPersistentFilesystem(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, approved := range approvedPersistentFilesystems {
		if value == approved {
			return true
		}
	}
	return false
}

// ApprovedPersistentFilesystemNames returns a defensive copy for table-driven
// policy checks and documentation tooling.
func ApprovedPersistentFilesystemNames() []string {
	result := make([]string, len(approvedPersistentFilesystems))
	copy(result, approvedPersistentFilesystems[:])
	return result
}
