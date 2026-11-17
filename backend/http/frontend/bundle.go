package frontend

import (
	"fmt"
	"io/fs"
)

func Validate(bundle fs.FS) error {
	if bundle == nil {
		return fmt.Errorf("frontend bundle is required")
	}
	index, err := fs.Stat(bundle, "index.html")
	if err != nil {
		return fmt.Errorf("frontend index: %w", err)
	}
	if !index.Mode().IsRegular() {
		return fmt.Errorf("frontend index must be a regular file")
	}
	assets, err := fs.Stat(bundle, "assets")
	if err != nil {
		return fmt.Errorf("frontend assets: %w", err)
	}
	if !assets.IsDir() {
		return fmt.Errorf("frontend assets must be a directory")
	}
	return nil
}
