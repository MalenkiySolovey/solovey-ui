package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"time"
)

const ManagementEndpointSchemaV1 = canonical.ManagementEndpointSchemaV1

const RecoveryPathSchemaV1 = canonical.RecoveryPathSchemaV1

type ManagementServiceKind = canonical.ManagementServiceKind

const ManagementPanel = canonical.ManagementPanel

const ManagementSSH = canonical.ManagementSSH

const ManagementSubscriptionAdmin = canonical.ManagementSubscriptionAdmin

const ManagementOtherAdmin = canonical.ManagementOtherAdmin

type ManagementEndpointV1 = canonical.ManagementEndpointV1

type RecoveryPathV1 = canonical.RecoveryPathV1

func ManagementEndpointBoundToResource(endpoint ManagementEndpointV1, resource ProtectableResource, now time.Time) bool {
	return canonical.ManagementEndpointBoundToResource(endpoint, resource, now)
}

func ManagementEndpointFromResource(resource ProtectableResource, kind ManagementServiceKind, now time.Time) ManagementEndpointV1 {
	return canonical.ManagementEndpointFromResource(resource, kind, now)
}

func ManagementEndpointCurrent(value ManagementEndpointV1, now time.Time) bool {
	return canonical.ManagementEndpointCurrent(value, now)
}

func RecoveryPathFresh(value RecoveryPathV1, now time.Time) bool {
	return canonical.RecoveryPathFresh(value, now)
}

func RecoveryPathValid(value RecoveryPathV1, now time.Time) bool {
	return canonical.RecoveryPathValid(value, now)
}
