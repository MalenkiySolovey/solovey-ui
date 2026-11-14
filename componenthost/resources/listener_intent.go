package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

const ConfiguredListenIntentSchemaV1 = canonical.ConfiguredListenIntentSchemaV1

type ListenIntentMode = canonical.ListenIntentMode

const ListenIntentExact = canonical.ListenIntentExact

const ListenIntentWildcard = canonical.ListenIntentWildcard

const ListenIntentDualStack = canonical.ListenIntentDualStack

type ConfiguredListenIntentV1 = canonical.ConfiguredListenIntentV1

func DeterministicConfiguredEndpointKeys(resource ProtectableResource) ([]PublicEndpointKey, bool) {
	return canonical.DeterministicConfiguredEndpointKeys(resource)
}

func ConfiguredListenIntents(resource ProtectableResource) ([]ConfiguredListenIntentV1, bool) {
	return canonical.ConfiguredListenIntents(resource)
}

func normalizeConfiguredListenIntents(resource ProtectableResource) (ConfiguredListenIntentV1, []ConfiguredListenIntentV1, bool) {
	return canonical.NormalizeConfiguredListenIntents(resource)
}

func BuildConfiguredListenIntent(resource ProtectableResource) ConfiguredListenIntentV1 {
	return canonical.BuildConfiguredListenIntent(resource)
}
