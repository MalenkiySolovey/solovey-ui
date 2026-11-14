package hostsurface

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
)

const SchemaV1 = canonical.SchemaV1

type Network = canonical.Network

type Family = canonical.Family

type Exposure = canonical.Exposure

type OwnershipMode = canonical.OwnershipMode

type Classification = canonical.Classification

type ManagementService = canonical.ManagementService

const NetworkTCP = canonical.NetworkTCP

const NetworkUDP = canonical.NetworkUDP

const NetworkUnknown = canonical.NetworkUnknown

const FamilyIPv4 = canonical.FamilyIPv4

const FamilyIPv6 = canonical.FamilyIPv6

const FamilyUnknown = canonical.FamilyUnknown

const ExposurePublic = canonical.ExposurePublic

const ExposurePrivate = canonical.ExposurePrivate

const ExposureLocal = canonical.ExposureLocal

const ExposureUnknown = canonical.ExposureUnknown

const OwnershipManaged = canonical.OwnershipManaged

const OwnershipExternalManaged = canonical.OwnershipExternalManaged

const OwnershipUnmanaged = canonical.OwnershipUnmanaged

const ClassificationExpectedManaged = canonical.ClassificationExpectedManaged

const ClassificationExpectedExternal = canonical.ClassificationExpectedExternal

const ClassificationLocalOnly = canonical.ClassificationLocalOnly

const ClassificationUnexpectedPublic = canonical.ClassificationUnexpectedPublic

const ClassificationUnknownOwner = canonical.ClassificationUnknownOwner

const ClassificationManagedExact = canonical.ClassificationManagedExact

const ClassificationForeign = canonical.ClassificationForeign

const ClassificationUnobserved = canonical.ClassificationUnobserved

const ClassificationStale = canonical.ClassificationStale

const ManagementServiceSSH = canonical.ManagementServiceSSH

type SemanticOwnerFactV1 = canonical.SemanticOwnerFactV1

type ProcessFact = canonical.ProcessFact

type ServiceFact = canonical.ServiceFact

type HostSurfaceFactV1 = canonical.HostSurfaceFactV1

func StableID(value HostSurfaceFactV1) string { return canonical.StableID(value) }

type Limits = canonical.Limits

func DefaultLimits() Limits { return canonical.DefaultLimits() }

type Observation = canonical.Observation

type Snapshot = canonical.Snapshot
