package resources

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
)

type ExpectedApplicationOwnerV1 = canonical.ExpectedApplicationOwnerV1

const ExpectedApplicationOwnerSchemaV1 = deploymentidentity.ExpectedApplicationOwnerSchemaV1

type CapabilityValue = canonical.CapabilityValue

const CapabilityUnknown = canonical.CapabilityUnknown

const CapabilityYes = canonical.CapabilityYes

const CapabilityNo = canonical.CapabilityNo

type ProtectableResourceCapabilities = canonical.ProtectableResourceCapabilities

type ProtectableResource = canonical.ProtectableResource

type ResourceWarning = canonical.ResourceWarning

type ResourceError = canonical.ResourceError

type ResourceSnapshot = canonical.ResourceSnapshot

type ResourceContributor interface {
	Owner() string
	ListProtectableResources(context.Context) ([]ProtectableResource, error)
}
