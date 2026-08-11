package recoverypath

// This file is a compatibility facade for callers of the original endpoint
// API. The neutral componenthost/management package is the sole owner of management
// endpoint projection and recovery-path validity.

import (
	"context"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func CurrentManagementEndpoints(ctx context.Context, now time.Time) []hostresources.ManagementEndpointV1 {
	return managementregistry.CurrentEndpoints(ctx, now)
}

func ManagementEndpoints(resources []hostresources.ProtectableResource, surfaces hostfacts.Snapshot, now time.Time) []hostresources.ManagementEndpointV1 {
	return managementregistry.Endpoints(resources, surfaces, now)
}

func ManagementEndpointFromSurface(surface hostfacts.HostSurfaceFactV1, now time.Time) hostresources.ManagementEndpointV1 {
	return managementregistry.EndpointFromSurface(surface, now)
}

func IsSSHSurface(surface hostfacts.HostSurfaceFactV1) bool {
	return managementregistry.IsSSHSurface(surface)
}

func Effective(value hostresources.RecoveryPathV1, endpoints []hostresources.ManagementEndpointV1, now time.Time) hostresources.RecoveryPathV1 {
	return managementregistry.Effective(value, endpoints, now)
}
