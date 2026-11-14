package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

func Fingerprint(resource ProtectableResource) string { return canonical.Fingerprint(resource) }

func Revision(value any) string { return canonical.Revision(value) }
