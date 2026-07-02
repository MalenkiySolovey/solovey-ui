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

const UpdateComponentID = "panel-update-ui"

type RuntimeManager struct {
	ConfigService *coreservice.ConfigService
	Catalog       Catalog
	Reconcile     func(context.Context) error
	Migrate       func(context.Context) error
	DropData      func(context.Context, string) error
	Timeout       time.Duration
}

func NewRuntimeManager(configService *coreservice.ConfigService) RuntimeManager {
	return RuntimeManager{
		ConfigService: configService,
		Catalog:       NewCatalog(),
		Reconcile:     coreservice.ReconcileComponents,
		Migrate:       coreservice.MigrateComponents,
		DropData:      coreservice.DropComponentData,
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
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	status, err := m.catalog().StatusByID(id)
	if err != nil {
		return ComponentStatus{}, err
	}
	if status.Installed {
		return status, nil
	}
	if !status.Installable {
		if status.UnavailableReason != "" {
			return ComponentStatus{}, fmt.Errorf("component %q cannot be installed: %s", id, status.UnavailableReason)
		}
		return ComponentStatus{}, fmt.Errorf("component %q cannot be installed in this runtime profile", id)
	}
	item, ok := findManifest(registeredManifests(), id)
	if !ok {
		return ComponentStatus{}, fmt.Errorf("component is not available in this binary: %s", id)
	}
	if err := m.ensureComponentPack(ctx, item); err != nil {
		return ComponentStatus{}, err
	}
	if _, err := installstate.SetInstalled(installstate.DefaultPath(), registeredManifests(), id, true); err != nil {
		return ComponentStatus{}, err
	}
	rollback := true
	defer func() {
		if rollback {
			_, _ = installstate.SetInstalled(installstate.DefaultPath(), registeredManifests(), id, false)
		}
	}()
	if err := m.migrate(); err != nil {
		return ComponentStatus{}, err
	}
	if err := m.reconcile(); err != nil {
		return ComponentStatus{}, err
	}
	rollback = false
	return componentByID(id)
}

func (m RuntimeManager) Remove(ctx OperationContext, id string, deleteData bool) (ComponentStatus, error) {
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	if id == UpdateComponentID {
		return ComponentStatus{}, fmt.Errorf("component %q cannot remove itself", id)
	}
	status, err := m.catalog().StatusByID(id)
	if err != nil {
		return ComponentStatus{}, err
	}
	if !status.Installed {
		return status, nil
	}
	if !status.Removable {
		if status.LockedReason != "" {
			return ComponentStatus{}, fmt.Errorf("component %q cannot be removed: %s", id, status.LockedReason)
		}
		return ComponentStatus{}, fmt.Errorf("component %q cannot be removed", id)
	}
	if _, err := installstate.SetInstalled(installstate.DefaultPath(), registeredManifests(), id, false); err != nil {
		return ComponentStatus{}, err
	}
	if err := m.reconcile(); err != nil {
		return ComponentStatus{}, err
	}
	if deleteData {
		if err := m.dropData(id); err != nil {
			return ComponentStatus{}, err
		}
	}
	if err := removeComponentPack(id); err != nil {
		return ComponentStatus{}, err
	}
	return componentByID(id)
}

func (m RuntimeManager) setEnabled(ctx OperationContext, id string, enabled bool) (ComponentStatus, error) {
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	if id == UpdateComponentID && !enabled {
		return ComponentStatus{}, fmt.Errorf("component %q cannot disable itself", id)
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

func (m RuntimeManager) migrate() error {
	migrate := m.Migrate
	if migrate == nil {
		migrate = coreservice.MigrateComponents
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return migrate(ctx)
}

func (m RuntimeManager) dropData(id string) error {
	dropData := m.DropData
	if dropData == nil {
		dropData = coreservice.DropComponentData
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return dropData(ctx, id)
}

func (m RuntimeManager) catalog() Catalog {
	if m.Catalog.ReleaseManifestFile == "" && m.Catalog.ReleaseManifestURL == "" && m.Catalog.HTTPClient == nil {
		return NewCatalog()
	}
	return m.Catalog
}

func (m RuntimeManager) ensureComponentPack(_ OperationContext, item manifest.Manifest) error {
	artifact, url, err := m.catalog().componentBundleArtifact()
	if err != nil || url == "" {
		return ensureManifestOnlyPack(item)
	}
	client := m.catalog().HTTPClient
	if client == nil {
		client = NewCatalog().HTTPClient
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	downloadCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return installComponentPackFromBundle(downloadCtx, client, url, artifact.SHA256, item.ID)
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
	required := requiredPanelVersion(item.Since)
	compatible := panelVersionCompatible(required)
	locked := item.ID == UpdateComponentID
	lockedReason := ""
	if locked {
		lockedReason = "the update component manages this screen and cannot manage itself"
	}
	unavailableReason := ""
	if !compatible {
		unavailableReason = fmt.Sprintf("requires panel %s or newer", required)
	}
	return ComponentStatus{
		ID:                item.ID,
		Name:              item.Name,
		Version:           item.Version,
		LatestVersion:     item.Version,
		Since:             item.Since,
		RequiredPanel:     required,
		Delivery:          item.Delivery,
		DefaultEnabled:    item.DefaultEnabled,
		TokenScopes:       append([]string(nil), item.TokenScopes...),
		AvailableInBinary: true,
		Compatible:        compatible,
		Locked:            locked,
		LockedReason:      lockedReason,
		Installable:       !isInstalled && compatible && !locked,
		Removable:         isInstalled && !locked,
		Installed:         isInstalled,
		Enabled:           isEnabled,
		Active:            isInstalled && isEnabled,
		UnavailableReason: unavailableReason,
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
