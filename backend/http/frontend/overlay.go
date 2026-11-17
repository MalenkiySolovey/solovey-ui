package frontend

import (
	"errors"
	"io/fs"
	"os"
)

type assetsFS struct {
	embedded    fs.FS
	directories func() ([]string, error)
}

func (assets assetsFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	file, err := assets.embedded.Open(name)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if assets.directories == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	componentAssets, err := assets.directories()
	if err != nil {
		return nil, err
	}
	for _, dir := range componentAssets {
		file, err := os.DirFS(dir).Open(name)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func NewAssetsFS(frontend fs.FS, directories func() ([]string, error)) (fs.FS, error) {
	embeddedAssets, err := fs.Sub(frontend, "assets")
	if err != nil {
		return nil, err
	}
	return Overlay(embeddedAssets, directories), nil
}

// Overlay serves the supplied bundle before the installed-owner asset directories.
func Overlay(embedded fs.FS, directories func() ([]string, error)) fs.FS {
	return assetsFS{embedded: embedded, directories: directories}
}
