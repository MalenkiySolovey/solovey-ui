package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

var errRemovalImpact = errors.New("component removal impact is unresolved")

// One read may outlive cancellation if a feature does not cooperate. Keep its
// slot until actual return; subsequent requests fail closed without new work.
var removalReads = make(chan struct{}, 1)

type RemovalAdmission struct {
	Preview   func(context.Context, string) (datalifecycle.Preview, error)
	Resources func(context.Context, string) (datalifecycle.Preview, error)
	Inventory func(context.Context) resources.ResourceSnapshot
	Admit     func(context.Context, string, func() error) error
}

func defaultRemovalAdmission() RemovalAdmission {
	return RemovalAdmission{Preview: datalifecycle.Shared().PreviewRemoval,
		Resources: datalifecycle.Shared().RemovalResources, Inventory: resources.Refresh,
		Admit: coreservice.AdmitComponentRemoval}
}

func (m RuntimeManager) commitMetadataRemoval(parent context.Context, id string, requireDisabled bool) error {
	if parent == nil {
		parent = context.Background()
	}
	timeout := 3 * time.Second
	if m.Timeout > 0 && m.Timeout < timeout {
		timeout = m.Timeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	admission := defaultRemovalAdmission()
	if m.Removal != nil {
		admission = *m.Removal
	}
	if admission.Preview == nil || admission.Resources == nil || admission.Inventory == nil || admission.Admit == nil {
		return errRemovalImpact
	}
	if err := boundedRemovalRead(ctx, func() error {
		preview, err := admission.Preview(ctx, id)
		if err != nil {
			return errRemovalImpact
		}
		if err := validateRemovalResources(id, preview, requireDisabled, true); err != nil {
			return err
		}
		return validateRemovalInventory(id, admission.Inventory(ctx))
	}); err != nil {
		return err
	}
	return operationcoordination.TrySerializeAdmission(func() error {
		return admission.Admit(ctx, id, func() error {
			preview, err := admission.Resources(ctx, id)
			if err != nil {
				return errRemovalImpact
			}
			if err := validateRemovalResources(id, preview, requireDisabled, false); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return errRemovalImpact
			}
			_, err = installstate.SetInstalled(installstate.DefaultPath(), registeredManifests(), id, false)
			return err
		})
	})
}

func boundedRemovalRead(ctx context.Context, read func() error) error {
	if ctx.Err() != nil {
		return errRemovalImpact
	}
	select {
	case removalReads <- struct{}{}:
	default:
		return errors.New("component removal observation is busy")
	}
	result := make(chan error, 1)
	go func() {
		defer func() { <-removalReads }()
		err := errRemovalImpact
		defer func() {
			if recover() != nil {
				err = errRemovalImpact
			}
			result <- err
		}()
		err = read()
	}()
	select {
	case err := <-result:
		if ctx.Err() != nil {
			return errRemovalImpact
		}
		return err
	case <-ctx.Done():
		return errRemovalImpact
	}
}

func validateRemovalResources(id string, preview datalifecycle.Preview, disabled, external bool) error {
	component, available := registry.ComponentByID(id)
	if !available || preview.Schema != "solovey.component-removal-impact/v1" || preview.OwnerID != id || !preview.Installed || !preview.Available {
		return errRemovalImpact
	}
	if disabled && preview.Enabled {
		return errors.New("disable component before removal")
	}
	for _, blocker := range preview.Blockers {
		if blocker == "owner_is_enabled" && !disabled {
			continue
		}
		if blocker == "owner_drop_contract_unavailable" && !component.Manifest.Database.Declared() && len(component.Manifest.Privileged.Entries) == 0 {
			continue
		}
		return errRemovalImpact
	}
	if len(preview.Resources) > 416 || preview.LeaseCount != 0 || len(preview.Operations) != 0 {
		return errRemovalImpact
	}
	for _, resource := range preview.Resources {
		if resource.Owner != id || resource.Terminal != "ABSENT" {
			return errors.New("component retains owned data; use the separate owner Drop Data workflow")
		}
	}
	if external {
		critical := len(component.Manifest.Privileged.Entries) != 0 || component.Manifest.Database.PortabilityClass == manifest.PortabilityHostBound
		for _, file := range component.Manifest.Database.Files {
			critical = critical || file.Portability == manifest.PortabilityHostBound
		}
		if preview.ExternalAuthority != "VERIFIED_SAFE" && (critical || preview.ExternalAuthority != "NOT_REQUIRED") {
			return errRemovalImpact
		}
	}
	return nil
}

func validateRemovalInventory(id string, snapshot resources.ResourceSnapshot) error {
	if len(snapshot.Errors) != 0 || len(snapshot.Warnings) != 0 || len(snapshot.Resources) > resources.MaxResourceFacts {
		return errRemovalImpact
	}
	for _, resource := range snapshot.Resources {
		if resource.Owner == "" || resource.ID == "" || resource.Source == "" {
			return errRemovalImpact
		}
		if resource.Owner == id || resource.ComponentID == id || strings.HasPrefix(resource.Capabilities.FallbackTargetID, id+":") {
			return errors.New("component retains a resource or external reference")
		}
	}
	return nil
}
