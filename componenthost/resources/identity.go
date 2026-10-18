package resources

import "github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"

const ResourceIdentitySchemaV1 = "solovey-ui/resource-identity/v1"
const ResourceObservationUnknown = "UNKNOWN"

const configuredResourceNotObserved = "configured_resource_not_observed"

// ResourceIdentityV1 binds read metadata to an existing configured resource.
// It supplies neither another owner nor runtime observation/cleanup authority.
// This initial contract admits Local configured inputs only.
type ResourceIdentityV1 struct {
	Schema            string      `json:"schema"`
	Host              hostref.Ref `json:"host"`
	Owner             string      `json:"owner"`
	ResourceID        string      `json:"resourceId"`
	DesiredRevision   string      `json:"desiredRevision"`
	ObservedRevision  string      `json:"observedRevision,omitempty"`
	ObservationStatus string      `json:"observationStatus"`
	ReasonCode        string      `json:"reasonCode"`
	Source            string      `json:"source"`
}

// LocalConfiguredIdentity wraps the canonical tuple without inventing a socket
// observation, timestamp, evidence reference or independent persistent ID.
func LocalConfiguredIdentity(resource ProtectableResource) *ResourceIdentityV1 {
	identity := &ResourceIdentityV1{
		Schema: ResourceIdentitySchemaV1, Host: hostref.Local(), Owner: resource.Owner,
		ResourceID: resource.ID, DesiredRevision: resource.Capabilities.ConfigRevision,
		ObservationStatus: ResourceObservationUnknown, ReasonCode: configuredResourceNotObserved,
		Source: resource.Source,
	}
	if !identity.ValidFor(resource) {
		return nil
	}
	return identity
}

// ValidFor rejects missing, stale, foreign or stronger-than-configured metadata.
// Parent identity fields remain canonical; absence means unknown, not Local or
// observed authority inferred for legacy contributors.
func (identity *ResourceIdentityV1) ValidFor(resource ProtectableResource) bool {
	if identity == nil {
		return false
	}
	return identity.Schema == ResourceIdentitySchemaV1 && identity.Host.IsLocal() &&
		identity.Owner == resource.Owner && identity.ResourceID == resource.ID &&
		identity.Source == resource.Source && identity.DesiredRevision == resource.Capabilities.ConfigRevision &&
		identityToken(identity.Owner, 256) && identityToken(identity.ResourceID, 256) &&
		identityToken(identity.Source, 128) && identityToken(identity.DesiredRevision, 128) &&
		identity.ObservedRevision == "" && identity.ObservationStatus == ResourceObservationUnknown &&
		identity.ReasonCode == configuredResourceNotObserved
}

func identityToken(value string, limit int) bool {
	return value != "" && optionalResourceToken(value, limit) == value
}
