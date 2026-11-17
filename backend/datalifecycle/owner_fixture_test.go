//go:build !minimal

package datalifecycle

import (
	"context"
	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"gorm.io/gorm"
)

type fixtureOwnerCatalog struct {
	manifest  componentmanifest.Manifest
	installed bool
}

func (f *fixtureOwnerCatalog) Installed() ([]string, error) {
	if f.installed {
		return []string{f.manifest.ID}, nil
	}
	return nil, nil
}
func (f *fixtureOwnerCatalog) Lookup(id string) (Owner, bool) {
	return Owner{Manifest: f.manifest, CanDrop: true}, id == f.manifest.ID
}
func (*fixtureOwnerCatalog) RemovalBlocker(context.Context, *gorm.DB, string) string { return "" }
