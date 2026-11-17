package web

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	frontend "github.com/MalenkiySolovey/solovey-ui/backend/http/frontend"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type assetsFS struct{ embedded fs.FS }

func (assets assetsFS) Open(name string) (fs.File, error) {
	return frontend.Overlay(assets.embedded, componentFrontendAssetDirs).Open(name)
}
func newAssetsFS(bundle fs.FS) (fs.FS, error) {
	return frontend.NewAssetsFS(bundle, componentFrontendAssetDirs)
}

// assetDirsCache memoizes the resolved component asset directories keyed by
// the installed-metadata file identity (path + mtime + size). Runtime pack
// install/remove rewrites installed.json atomically, so a single os.Stat per
// request is enough to detect any change — including writes made by the
// installer outside this process — without re-reading and re-validating the
// metadata on every asset request.
var assetDirsCache struct {
	sync.Mutex
	path    string
	modTime time.Time
	size    int64
	dirs    []string
	valid   bool
}

func componentFrontendAssetDirs() ([]string, error) {
	metadataPath := installstate.DefaultPath()
	info, err := os.Stat(metadataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	assetDirsCache.Lock()
	defer assetDirsCache.Unlock()
	if assetDirsCache.valid &&
		assetDirsCache.path == metadataPath &&
		assetDirsCache.modTime.Equal(info.ModTime()) &&
		assetDirsCache.size == info.Size() {
		return assetDirsCache.dirs, nil
	}

	dirs, err := resolveComponentFrontendAssetDirs(metadataPath)
	if err != nil {
		assetDirsCache.valid = false
		return nil, err
	}
	assetDirsCache.path = metadataPath
	assetDirsCache.modTime = info.ModTime()
	assetDirsCache.size = info.Size()
	assetDirsCache.dirs = dirs
	assetDirsCache.valid = true
	return dirs, nil
}

func resolveComponentFrontendAssetDirs(metadataPath string) ([]string, error) {
	metadata, exists, err := installstate.Load(metadataPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	// The installed-component metadata and the component packs are one
	// package-owned generation. Resolve packs from that authority instead of
	// assuming they live beside the mutable database. Package-managed OpenWrt
	// deliberately keeps the database under /etc while immutable component
	// assets live with installed.json under /usr/lib.
	componentsDir := filepath.Dir(metadataPath)
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
