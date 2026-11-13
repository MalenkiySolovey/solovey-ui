package resources

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"

const ResourceIdentitySchemaV1 = canonical.ResourceIdentitySchemaV1
const ResourceObservationUnknown = canonical.ResourceObservationUnknown

// ResourceIdentityV1 retains Product Inventory's ValidFor adapter while using
// the shared configured identity value and validation implementation.
type ResourceIdentityV1 canonical.ResourceIdentityV1

func configuredTuple(r ProtectableResource) canonical.ConfiguredResource {
	return canonical.ConfiguredResource{ID: r.ID, Owner: r.Owner, Source: r.Source, Revision: r.Capabilities.ConfigRevision}
}
func LocalConfiguredIdentity(r ProtectableResource) *ResourceIdentityV1 {
	return (*ResourceIdentityV1)(canonical.LocalConfiguredIdentity(configuredTuple(r)))
}
func (i *ResourceIdentityV1) ValidFor(r ProtectableResource) bool {
	return (*canonical.ResourceIdentityV1)(i).ValidFor(configuredTuple(r))
}
