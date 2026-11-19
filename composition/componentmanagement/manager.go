// Package componentmanagement binds the shared ComponentHost manager to the
// Product's installed metadata, supervisor and Panel lifecycle adapters.
package componentmanagement

import (
	"context"

	operationcoordination "github.com/MalenkiySolovey/solovey-backend/operations/coordination"
	host "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

func New(data *datalifecycle.Manager) host.Manager {
	if data == nil {
		data = datalifecycle.Shared()
	}
	return host.Manager{
		Snapshot:     catalogSnapshot,
		SetInstalled: writeComponentInstalled,
		Reconcile: func(ctx context.Context) error {
			if err := service.ReconcileComponents(ctx); err != nil {
				return err
			}
			state.InvalidateActiveCache()
			return nil
		},
		Migrate: service.MigrateComponents,
		Removal: host.RemovalAdmission{
			Preview:   removalImpact(data.PreviewRemoval),
			Resources: removalImpact(data.RemovalResources),
			Inventory: resources.Refresh,
			Serialize: operationcoordination.TrySerializeAdmission,
			Admit:     service.AdmitComponentRemoval,
		},
	}
}

func componentManifests() []manifest.Manifest {
	components := registry.Components()
	manifests := make([]manifest.Manifest, 0, len(components))
	for _, component := range components {
		manifests = append(manifests, component.Manifest)
	}
	return manifests
}

func writeComponentInstalled(id string, installed bool) error {
	_, err := installstate.SetInstalled(installstate.DefaultPath(), componentManifests(), id, installed)
	return err
}

func catalogSnapshot() (host.CatalogSnapshot, error) {
	components, err := state.Components()
	if err != nil {
		return host.CatalogSnapshot{}, err
	}
	installed := make([]host.InstalledOwner, 0, len(components))
	for _, component := range components {
		installed = append(installed, host.InstalledOwner{ID: component.ID, Delivery: component.Delivery, Enabled: component.Enabled})
	}
	version := identity.GetVersion()
	result := host.CatalogSnapshot{
		BinaryProfile: componentprofile.Binary,
		Manifests:     componentManifests(), Installed: installed,
		Compatible: func(required string) bool {
			comparison, ok := versionpolicy.CompareVersions(version, required)
			return ok && comparison >= 0
		},
		Observe: func(id string, installed, available bool) host.LifecycleObservation {
			return host.LifecycleObservation(state.LifecycleForComponent(id, installed, available))
		},
		LockedID: installstate.FormerPanelUpdateID, LockedReason: "Panel Update is built in",
	}
	if !installstate.RuntimeMutable() {
		result.PackageLockReason = installstate.ErrPackageManaged.Error()
	}
	return result, nil
}

func removalImpact(read func(context.Context, string) (datalifecycle.Preview, error)) func(context.Context, string) (host.RemovalImpact, error) {
	return func(ctx context.Context, id string) (host.RemovalImpact, error) {
		preview, err := read(ctx, id)
		if err != nil {
			return host.RemovalImpact{}, err
		}
		impact := host.RemovalImpact{Schema: preview.Schema, OwnerID: preview.OwnerID, Installed: preview.Installed,
			Available: preview.Available, Enabled: preview.Enabled, Blockers: preview.Blockers,
			OperationCount: len(preview.Operations), ExternalAuthority: preview.ExternalAuthority,
			Resources: make([]host.RemovalResource, len(preview.Resources))}
		if preview.LeaseCount != 0 {
			impact.LeaseCount = 1 // Preserve nonzero leases on 32-bit builds too.
		}
		for i, resource := range preview.Resources {
			impact.Resources[i] = host.RemovalResource{Owner: resource.Owner, Terminal: resource.Terminal}
		}
		return impact, nil
	}
}
