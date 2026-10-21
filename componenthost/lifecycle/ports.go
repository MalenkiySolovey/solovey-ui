package lifecycle

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

// PortLifecycle uses the same supervisor and observed lifecycle as legacy
// in-process Components, with a declared, narrow dependency boundary.
type PortLifecycle interface {
	Start(context.Context, componenthost.Ports) error
	Stop(context.Context) error
}

type PortDataDropper interface {
	DropData(context.Context, componenthost.Ports) error
}

type portAdapter struct {
	manifest manifest.Manifest
	owner    PortLifecycle
}

func BindPorts(descriptor manifest.Manifest, owner PortLifecycle) (Lifecycle, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	// Legacy hooks carry Context or raw database handles. Reject rather than
	// silently losing them when a Component opts into the typed boundary.
	if owner == nil {
		return nil, fmt.Errorf("typed lifecycle is required")
	}
	if _, ok := owner.(Migrator); ok {
		return nil, fmt.Errorf("legacy migration hook requires owner migration before typed admission")
	}
	if _, ok := owner.(StagedMigrator); ok {
		return nil, fmt.Errorf("legacy staged migration hook is not a typed port")
	}
	if _, ok := owner.(RestoreRehearser); ok {
		return nil, fmt.Errorf("legacy restore hook is not a typed port")
	}
	if _, ok := owner.(BackupExclusions); ok {
		return nil, fmt.Errorf("legacy backup hook is not a typed port")
	}
	if _, ok := owner.(DropAuthorityInspector); ok {
		return nil, fmt.Errorf("legacy drop inspection hook is not a typed port")
	}
	if _, ok := owner.(DataDropper); ok {
		return nil, fmt.Errorf("legacy data-drop hook is not a typed port")
	}
	if descriptor.HostAPI == nil {
		return nil, fmt.Errorf("typed Host API contract is required")
	}
	descriptor = descriptor.Normalized()
	adapter := portAdapter{manifest: descriptor, owner: owner}
	if dropper, ok := owner.(PortDataDropper); ok {
		return portDropAdapter{portAdapter: adapter, dropper: dropper}, nil
	}
	if len(descriptor.HostAPI.DropData) != 0 {
		return nil, fmt.Errorf("declared data-drop ports require typed data-drop hook")
	}
	return adapter, nil
}

func (a portAdapter) Start(ctx context.Context, host Context) error {
	ports, err := host.Host.BindLifecycle(*a.manifest.HostAPI)
	if err != nil {
		return err
	}
	return a.owner.Start(ctx, ports)
}

func (a portAdapter) Stop(ctx context.Context) error { return a.owner.Stop(ctx) }

type portDropAdapter struct {
	portAdapter
	dropper PortDataDropper
}

func (a portDropAdapter) DropData(ctx context.Context, host Context) error {
	ports, err := host.Host.BindDataDrop(a.manifest)
	if err != nil {
		return err
	}
	return a.dropper.DropData(ctx, ports)
}
