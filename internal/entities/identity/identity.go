// Package identity forwards persisted identity invariants to the Panel owner.
package identity

import (
	owner "github.com/MalenkiySolovey/solovey-ui/backend/storage/entityidentity"
	"gorm.io/gorm"
)

func ValidateTypeTag(kind, tag string) error { return owner.ValidateTypeTag(kind, tag) }
func ValidateTag(tag string) error           { return owner.ValidateTag(tag) }
func ValidateName(name string) error         { return owner.ValidateName(name) }
func EnsureOutboundTagAvailable(tx *gorm.DB, tag string, outbound, endpoint uint) error {
	return owner.EnsureOutboundTagAvailable(tx, tag, outbound, endpoint)
}
func ValidateStored(tx *gorm.DB) error { return owner.ValidateStored(tx) }
