package installstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
)

const InstalledFileEnv = "SUI_COMPONENTS_INSTALLED_FILE"

const (
	InstalledMetadataVersion  = 1
	MaxInstalledMetadataBytes = 1 << 20
	MaxInstalledComponents    = 256
)

type Metadata struct {
	Version    int                  `json:"version"`
	Profile    string               `json:"profile,omitempty"`
	Binary     string               `json:"binary,omitempty"`
	Components []InstalledComponent `json:"components"`
}

type InstalledComponent struct {
	ID        string            `json:"id"`
	Delivery  manifest.Delivery `json:"delivery"`
	Installed bool              `json:"installed"`
}

func DefaultPath() string {
	if path := os.Getenv(InstalledFileEnv); path != "" {
		return path
	}
	dbFolder := configstorage.GetDBFolderPath()
	return filepath.Join(filepath.Dir(dbFolder), "components", "installed.json")
}

func InstalledIDs(available []manifest.Manifest) (map[string]struct{}, error) {
	metadata, exists, err := Load(DefaultPath())
	if err != nil {
		return nil, err
	}
	availableIDs := make(map[string]manifest.Manifest, len(available))
	for _, item := range available {
		availableIDs[item.ID] = item
	}
	if !exists {
		return map[string]struct{}{}, nil
	}

	ids := map[string]struct{}{}
	for _, item := range metadata.Components {
		if err := manifest.ValidateID(item.ID); err != nil {
			return nil, err
		}
		if !item.Installed {
			continue
		}
		availableManifest, ok := availableIDs[item.ID]
		if !ok {
			continue
		}
		if item.Delivery != "" && item.Delivery != availableManifest.Delivery {
			return nil, fmt.Errorf("installed component %q delivery %q does not match binary delivery %q", item.ID, item.Delivery, availableManifest.Delivery)
		}
		ids[item.ID] = struct{}{}
	}
	return ids, nil
}

func InstalledComponents() ([]InstalledComponent, error) {
	metadata, exists, err := Load(DefaultPath())
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	components := make(map[string]InstalledComponent, len(metadata.Components))
	for _, item := range metadata.Components {
		if err := manifest.ValidateID(item.ID); err != nil {
			return nil, err
		}
		if _, duplicate := components[item.ID]; duplicate {
			return nil, fmt.Errorf("component %q is duplicated in installed metadata", item.ID)
		}
		if !item.Installed {
			continue
		}
		components[item.ID] = item
	}
	return sortedInstalledComponents(components), nil
}

func Load(path string) (Metadata, bool, error) {
	if path == "" {
		return Metadata{}, false, errors.New("component installed metadata path is empty")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Metadata{}, false, nil
		}
		return Metadata{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxInstalledMetadataBytes {
		return Metadata{}, true, errors.New("component installed metadata file is invalid")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from installation metadata/env override.
	if err != nil {
		return Metadata{}, true, err
	}
	var metadata Metadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return Metadata{}, true, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Metadata{}, true, errors.New("component installed metadata contains multiple JSON values")
	}
	if err := validateMetadata(metadata); err != nil {
		return Metadata{}, true, err
	}
	return metadata, true, nil
}

func SetInstalled(path string, available []manifest.Manifest, id string, installed bool) (Metadata, error) {
	if err := manifest.ValidateID(id); err != nil {
		return Metadata{}, err
	}
	availableByID := make(map[string]manifest.Manifest, len(available))
	for _, item := range available {
		if err := item.Validate(); err != nil {
			return Metadata{}, err
		}
		availableByID[item.ID] = item
	}
	target, ok := availableByID[id]
	if !ok {
		return Metadata{}, fmt.Errorf("component %q is not available in this binary", id)
	}

	metadata, exists, err := Load(path)
	if err != nil {
		return Metadata{}, err
	}
	if !exists {
		metadata = Metadata{
			Version: 1,
			Profile: defaultProfile(available),
			Binary:  componentprofile.Binary,
		}
	}
	if metadata.Version <= 0 {
		metadata.Version = 1
	}
	if metadata.Profile == "" {
		metadata.Profile = defaultProfile(available)
	}
	if metadata.Binary == "" {
		metadata.Binary = componentprofile.Binary
	}

	components := make(map[string]InstalledComponent, len(metadata.Components)+1)
	for _, item := range metadata.Components {
		if err := manifest.ValidateID(item.ID); err != nil {
			return Metadata{}, err
		}
		if _, duplicate := components[item.ID]; duplicate {
			return Metadata{}, fmt.Errorf("component %q is duplicated in installed metadata", item.ID)
		}
		if item.Installed {
			availableItem, ok := availableByID[item.ID]
			if !ok {
				components[item.ID] = item
				continue
			}
			if item.Delivery != "" && item.Delivery != availableItem.Delivery {
				return Metadata{}, fmt.Errorf("installed component %q delivery %q does not match binary delivery %q", item.ID, item.Delivery, availableItem.Delivery)
			}
		}
		components[item.ID] = item
	}
	if installed {
		components[id] = InstalledComponent{
			ID:        id,
			Delivery:  target.Delivery,
			Installed: true,
		}
	} else {
		delete(components, id)
	}

	metadata.Components = sortedInstalledComponents(components)
	if err := Store(path, metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func Store(path string, metadata Metadata) error {
	if path == "" {
		return errors.New("component installed metadata path is empty")
	}
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil || len(data)+1 > MaxInstalledMetadataBytes {
		if err == nil {
			err = errors.New("component installed metadata exceeds the size limit")
		}
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}

func validateMetadata(metadata Metadata) error {
	if metadata.Version != InstalledMetadataVersion {
		return errors.New("component installed metadata version is unsupported")
	}
	if len(metadata.Components) > MaxInstalledComponents || !safeMetadataLabel(metadata.Profile) || !safeMetadataLabel(metadata.Binary) {
		return errors.New("component installed metadata inventory is invalid")
	}
	seen := make(map[string]struct{}, len(metadata.Components))
	for _, item := range metadata.Components {
		if err := manifest.ValidateID(item.ID); err != nil {
			return err
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return fmt.Errorf("component %q is duplicated in installed metadata", item.ID)
		}
		seen[item.ID] = struct{}{}
		if item.Delivery != "" && item.Delivery != manifest.DeliveryInProcess {
			return fmt.Errorf("component %q has unsupported installed delivery %q", item.ID, item.Delivery)
		}
	}
	return nil
}

func safeMetadataLabel(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func defaultProfile(available []manifest.Manifest) string {
	if len(available) == 0 || componentprofile.Binary == "core" {
		return "core"
	}
	return "full"
}

func sortedInstalledComponents(components map[string]InstalledComponent) []InstalledComponent {
	ids := make([]string, 0, len(components))
	for id := range components {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]InstalledComponent, 0, len(ids))
	for _, id := range ids {
		result = append(result, components[id])
	}
	return result
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".installed-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
