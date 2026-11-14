package resources

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"

const ResourceIdentitySchemaV1 = canonical.ResourceIdentitySchemaV1
const ResourceObservationUnknown = canonical.ResourceObservationUnknown

type ResourceIdentityV1 = canonical.ResourceIdentityV1

func LocalConfiguredIdentity(r ProtectableResource) *ResourceIdentityV1 {
	return canonical.LocalConfiguredIdentity(r.ConfiguredResource())
}
