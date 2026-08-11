package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
)

const (
	ReleaseManifestFileEnv = "SUI_COMPONENT_RELEASE_MANIFEST_FILE"
	ReleaseManifestURLEnv  = "SUI_COMPONENT_RELEASE_MANIFEST_URL"
	releaseDownloadBase    = "https://github.com/MalenkiySolovey/solovey-ui/releases/download"
	releaseManifestName    = "solovey-ui-release.json"
	releaseHTTPTimeout     = 5 * time.Second
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Catalog struct {
	ReleaseManifestFile string
	ReleaseManifestURL  string
	HTTPClient          HTTPDoer
}

func NewCatalog() Catalog {
	return Catalog{
		ReleaseManifestFile: os.Getenv(ReleaseManifestFileEnv),
		ReleaseManifestURL:  os.Getenv(ReleaseManifestURLEnv),
		HTTPClient:          ssrf.NewHTTPClient(releaseHTTPTimeout, "https"),
	}
}

func (c Catalog) Inventory() (Inventory, error) {
	registered := registeredManifests()
	statuses, err := statusesForManifests(registered)
	if err != nil {
		return Inventory{}, err
	}
	release, source, err := c.loadReleaseCatalog()
	if err != nil {
		release = releaseCatalog{Components: map[string]releaseComponent{}}
	}

	inventory := Inventory{
		BinaryProfile:  componentprofile.Binary,
		ReleaseVersion: release.Version,
		ReleaseSource:  source,
	}
	if err != nil {
		inventory.ReleaseError = "release_catalog_unavailable"
	}
	seen := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		seen[status.ID] = struct{}{}
		if releaseComponent, ok := release.Components[status.ID]; ok {
			mergeReleaseMetadata(&status, releaseComponent)
		}
		if status.Installed {
			status.Group = GroupInstalled
			inventory.Installed = append(inventory.Installed, status)
		} else {
			status.Group = GroupAvailable
			inventory.Available = append(inventory.Available, status)
		}
		inventory.Components = append(inventory.Components, status)
	}

	for id, releaseComponent := range release.Components {
		if _, ok := seen[id]; ok {
			continue
		}
		status := unavailableStatus(id, releaseComponent)
		inventory.Unavailable = append(inventory.Unavailable, status)
	}

	sortInventory(&inventory)
	return inventory, nil
}

func (c Catalog) StatusByID(id string) (ComponentStatus, error) {
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	inventory, err := c.Inventory()
	if err != nil {
		return ComponentStatus{}, err
	}
	for _, status := range inventory.Components {
		if status.ID == id {
			return status, nil
		}
	}
	for _, status := range inventory.Unavailable {
		if status.ID == id {
			return status, nil
		}
	}
	return ComponentStatus{}, fmt.Errorf("component is not available in this profile: %s", id)
}

func (c Catalog) loadReleaseCatalog() (releaseCatalog, string, error) {
	path := strings.TrimSpace(c.ReleaseManifestFile)
	if path == "" {
		return c.fetchReleaseCatalog()
	}
	data, err := os.ReadFile(path) // #nosec G304 -- update component reads the installer-provided release manifest path.
	if err != nil {
		return releaseCatalog{}, path, err
	}
	catalog, err := decodeReleaseCatalog(data)
	return catalog, path, err
}

func (c Catalog) fetchReleaseCatalog() (releaseCatalog, string, error) {
	requestURL := strings.TrimSpace(c.ReleaseManifestURL)
	if requestURL == "" {
		requestURL = defaultReleaseManifestURL(configidentity.GetVersion())
	}
	if requestURL == "" {
		return releaseCatalog{Components: map[string]releaseComponent{}}, "", nil
	}
	client := c.HTTPClient
	if client == nil {
		client = ssrf.NewHTTPClient(releaseHTTPTimeout, "https")
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseHTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return releaseCatalog{}, requestURL, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "solovey-ui-component-catalog")
	response, err := client.Do(request)
	if err != nil {
		return releaseCatalog{}, requestURL, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return releaseCatalog{}, requestURL, fmt.Errorf("release manifest status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return releaseCatalog{}, requestURL, err
	}
	catalog, err := decodeReleaseCatalog(data)
	return catalog, requestURL, err
}

func decodeReleaseCatalog(data []byte) (releaseCatalog, error) {
	var catalog releaseCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return releaseCatalog{}, err
	}
	if catalog.Components == nil {
		catalog.Components = map[string]releaseComponent{}
	}
	return catalog, nil
}

func defaultReleaseManifestURL(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	tag := version
	if !strings.HasPrefix(strings.ToLower(tag), "v") {
		tag = "v" + tag
	}
	return fmt.Sprintf("%s/%s/%s", releaseDownloadBase, tag, releaseManifestName)
}

type releaseCatalog struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Version       string                      `json:"version"`
	Linux         map[string]releaseArtifacts `json:"linux"`
	Components    map[string]releaseComponent `json:"components"`
}

type releaseArtifacts struct {
	Components *releaseArtifact `json:"components,omitempty"`
}

type releaseArtifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type releaseComponent struct {
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Since          string            `json:"since"`
	Delivery       manifest.Delivery `json:"delivery"`
	DefaultEnabled bool              `json:"defaultEnabled"`
	TokenScopes    []string          `json:"tokenScopes"`
}

func mergeReleaseMetadata(status *ComponentStatus, release releaseComponent) {
	if release.Name != "" {
		status.Name = release.Name
	}
	if release.Version != "" {
		status.LatestVersion = release.Version
	}
	if release.Since != "" {
		status.Since = release.Since
	}
	if release.Delivery != "" {
		status.Delivery = release.Delivery
	}
	if len(release.TokenScopes) > 0 {
		status.TokenScopes = append([]string(nil), release.TokenScopes...)
	}
	status.RequiredPanel = requiredPanelVersion(status.Since)
	status.Compatible = panelVersionCompatible(status.RequiredPanel)
	if !status.Compatible {
		status.Installable = false
		status.Removable = false
		status.UnavailableReason = fmt.Sprintf("requires panel %s or newer", status.RequiredPanel)
	}
}

func unavailableStatus(id string, release releaseComponent) ComponentStatus {
	name := strings.TrimSpace(release.Name)
	if name == "" {
		name = id
	}
	delivery := release.Delivery
	if delivery == "" {
		delivery = manifest.DeliveryInProcess
	}
	return ComponentStatus{
		ID:                id,
		Name:              name,
		Version:           release.Version,
		LatestVersion:     release.Version,
		Since:             release.Since,
		RequiredPanel:     requiredPanelVersion(release.Since),
		Delivery:          delivery,
		DefaultEnabled:    release.DefaultEnabled,
		TokenScopes:       append([]string(nil), release.TokenScopes...),
		AvailableInBinary: false,
		Compatible:        panelVersionCompatible(requiredPanelVersion(release.Since)),
		Installable:       false,
		Removable:         false,
		Installed:         false,
		Enabled:           false,
		Active:            false,
		Group:             GroupUnavailable,
		UnavailableReason: unavailableReasonForReleaseOnly(release),
	}
}

func unavailableReasonForReleaseOnly(release releaseComponent) string {
	required := requiredPanelVersion(release.Since)
	if !panelVersionCompatible(required) {
		return fmt.Sprintf("requires panel %s or newer", required)
	}
	return "not bundled in this binary profile"
}

func requiredPanelVersion(since string) string {
	return strings.TrimSpace(since)
}

func panelVersionCompatible(required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return true
	}
	comparison, ok := versionpolicy.CompareVersions(configidentity.GetVersion(), required)
	return ok && comparison >= 0
}

func (c Catalog) componentBundleArtifact() (releaseArtifact, string, error) {
	release, source, err := c.loadReleaseCatalog()
	if err != nil {
		return releaseArtifact{}, source, err
	}
	artifact := releaseArtifact{}
	for _, artifacts := range release.Linux {
		if artifacts.Components != nil && artifacts.Components.Name != "" {
			artifact = *artifacts.Components
			break
		}
	}
	if artifact.Name == "" {
		return releaseArtifact{}, source, fmt.Errorf("release component bundle is not listed")
	}
	if strings.TrimSpace(artifact.SHA256) == "" {
		return releaseArtifact{}, source, fmt.Errorf("release component bundle checksum is not listed")
	}
	tagVersion := release.Version
	if tagVersion == "" {
		tagVersion = configidentity.GetVersion()
	}
	return artifact, componentBundleURL(tagVersion, artifact.Name), nil
}

func componentBundleURL(version string, artifact string) string {
	version = strings.TrimSpace(version)
	if version == "" || artifact == "" {
		return ""
	}
	tag := version
	if !strings.HasPrefix(strings.ToLower(tag), "v") {
		tag = "v" + tag
	}
	return fmt.Sprintf("%s/%s/%s", releaseDownloadBase, tag, artifact)
}

func sortInventory(inventory *Inventory) {
	sortStatuses(inventory.Components)
	sortStatuses(inventory.Installed)
	sortStatuses(inventory.Available)
	sortStatuses(inventory.Unavailable)
}

func sortStatuses(items []ComponentStatus) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}
