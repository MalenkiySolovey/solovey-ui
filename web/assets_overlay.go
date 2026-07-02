package web

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type overlayFS struct {
	roots []fs.FS
}

func (overlay overlayFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	for _, root := range overlay.roots {
		file, err := root.Open(name)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func newAssetsFS() (fs.FS, error) {
	embeddedAssets, err := fs.Sub(content, "html/assets")
	if err != nil {
		return nil, err
	}

	roots := []fs.FS{embeddedAssets}
	componentAssets, err := componentFrontendAssetDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range componentAssets {
		roots = append(roots, os.DirFS(dir))
	}
	return overlayFS{roots: roots}, nil
}

func componentFrontendAssetDirs() ([]string, error) {
	metadata, exists, err := installstate.Load(installstate.DefaultPath())
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	componentsDir := filepath.Join(filepath.Dir(configstorage.GetDBFolderPath()), "components")
	dirs := make([]string, 0, len(metadata.Components))
	for _, component := range metadata.Components {
		if !component.Installed {
			continue
		}
		if err := manifest.ValidateID(component.ID); err != nil {
			return nil, err
		}

		packDir := filepath.Join(componentsDir, component.ID)
		packInfo, err := os.Stat(packDir)
		if err != nil {
			return nil, err
		}
		if !packInfo.IsDir() {
			return nil, &fs.PathError{Op: "stat", Path: packDir, Err: fs.ErrInvalid}
		}
		if _, err := os.Stat(filepath.Join(packDir, "component.json")); err != nil {
			return nil, err
		}

		assetDir := filepath.Join(componentsDir, component.ID, "frontend", "assets")
		info, err := os.Stat(assetDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, &fs.PathError{Op: "stat", Path: assetDir, Err: fs.ErrInvalid}
		}
		dirs = append(dirs, assetDir)
	}
	sort.Strings(dirs)
	return dirs, nil
}
