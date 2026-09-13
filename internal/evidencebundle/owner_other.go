//go:build !linux

package evidencebundle

import "os"

// Non-Linux hosts can validate schema/integrity fixtures. Root ownership is a
// Linux target fact and is enforced by owner_linux.go.
func validateEvidenceOwner(os.FileInfo) error       { return nil }
func validateEvidencePermissions(os.FileInfo) error { return nil }
