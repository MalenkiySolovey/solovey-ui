package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

type RuntimeManager struct {
	ConfigService *coreservice.ConfigService
	Reconcile     func(context.Context) error
	Timeout       time.Duration
}

func NewRuntimeManager(configService *coreservice.ConfigService) RuntimeManager {
	return RuntimeManager{
		ConfigService: configService,
		Reconcile:     coreservice.ReconcileComponents,
		Timeout:       10 * time.Second,
	}
}

func (m RuntimeManager) Enable(ctx OperationContext, id string) (ComponentStatus, error) {
	return m.setEnabled(ctx, id, true)
}

func (m RuntimeManager) Disable(ctx OperationContext, id string) (ComponentStatus, error) {
	return m.setEnabled(ctx, id, false)
}

func (m RuntimeManager) Install(ctx OperationContext, id string) (ComponentStatus, error) {
	if _, err := componentByID(id); err != nil {
		return ComponentStatus{}, err
	}
	return ComponentStatus{}, fmt.Errorf("component %q cannot be installed from a running panel; install packs through the installer/update flow and restart the panel", id)
}

func (m RuntimeManager) Remove(ctx OperationContext, id string) (ComponentStatus, error) {
	if _, err := componentByID(id); err != nil {
		return ComponentStatus{}, err
	}
	return ComponentStatus{}, fmt.Errorf("component %q cannot be removed from a running panel; remove packs through the installer/update flow and restart the panel", id)
}

func (m RuntimeManager) setEnabled(ctx OperationContext, id string, enabled bool) (ComponentStatus, error) {
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	status, err := componentByID(id)
	if err != nil {
		return ComponentStatus{}, err
	}
	if !status.Installed {
		return ComponentStatus{}, fmt.Errorf("component is not installed: %s", id)
	}
	payload, err := json.Marshal(map[string]string{
		enabledstate.SettingKey(id): strconv.FormatBool(enabled),
	})
	if err != nil {
		return ComponentStatus{}, err
	}
	configService := m.ConfigService
	if configService == nil {
		configService = &coreservice.ConfigService{}
	}
	if _, err := configService.Save("settings", "set", payload, "", ctx.Actor, ctx.Hostname); err != nil {
		return ComponentStatus{}, err
	}
	if err := m.reconcile(); err != nil {
		return ComponentStatus{}, err
	}
	state.InvalidateActiveCache()
	return componentByID(id)
}

func (m RuntimeManager) reconcile() error {
	reconcile := m.Reconcile
	if reconcile == nil {
		reconcile = coreservice.ReconcileComponents
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := reconcile(ctx); err != nil {
		return err
	}
	state.InvalidateActiveCache()
	return nil
}

func componentByID(id string) (ComponentStatus, error) {
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	manifests := registeredManifests()
	item, ok := findManifest(manifests, id)
	if !ok {
		return ComponentStatus{}, fmt.Errorf("component is not available in this binary: %s", id)
	}
	installed, enabled, err := componentRuntimeState(manifests)
	if err != nil {
		return ComponentStatus{}, err
	}
	return statusForManifest(item, hasID(installed, id), hasID(enabled, id)), nil
}

func statusesForManifests(manifests []manifest.Manifest) ([]ComponentStatus, error) {
	installed, enabled, err := componentRuntimeState(registeredManifests())
	if err != nil {
		return nil, err
	}
	statuses := make([]ComponentStatus, 0, len(manifests))
	for _, item := range manifests {
		status := statusForManifest(item, hasID(installed, item.ID), hasID(enabled, item.ID))
		status.Group = GroupAvailable
		if status.Installed {
			status.Group = GroupInstalled
		}
		statuses = append(statuses, status)
	}
	sortComponents(statuses)
	return statuses, nil
}

func componentRuntimeState(manifests []manifest.Manifest) (map[string]struct{}, map[string]struct{}, error) {
	installed, err := installstate.InstalledIDs(manifests)
	if err != nil {
		return nil, nil, err
	}
	installedManifests := make([]manifest.Manifest, 0, len(manifests))
	for _, item := range manifests {
		if hasID(installed, item.ID) {
			installedManifests = append(installedManifests, item)
		}
	}
	enabled, err := enabledstate.EnabledIDs(installedManifests)
	if err != nil {
		return nil, nil, err
	}
	return installed, enabled, nil
}

func statusForManifest(item manifest.Manifest, isInstalled bool, isEnabled bool) ComponentStatus {
	return ComponentStatus{
		ID:                item.ID,
		Name:              item.Name,
		Version:           item.Version,
		LatestVersion:     item.Version,
		Since:             item.Since,
		Delivery:          item.Delivery,
		DefaultEnabled:    item.DefaultEnabled,
		TokenScopes:       append([]string(nil), item.TokenScopes...),
		AvailableInBinary: true,
		Installable:       false,
		Removable:         false,
		Installed:         isInstalled,
		Enabled:           isEnabled,
		Active:            isInstalled && isEnabled,
	}
}

func registeredManifests() []manifest.Manifest {
	components := registry.Components()
	manifests := make([]manifest.Manifest, 0, len(components))
	for _, component := range components {
		manifests = append(manifests, component.Manifest)
	}
	return manifests
}

func findManifest(manifests []manifest.Manifest, id string) (manifest.Manifest, bool) {
	for _, item := range manifests {
		if item.ID == id {
			return item, true
		}
	}
	return manifest.Manifest{}, false
}

func hasID(ids map[string]struct{}, id string) bool {
	_, ok := ids[id]
	return ok
}

func sortComponents(items []ComponentStatus) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}
