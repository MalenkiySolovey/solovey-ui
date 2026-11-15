// Package productsource locates the actual Product inputs used by owner
// integration tests, independently of the external feature's package directory.
package productsource

import (
	"path/filepath"
	"runtime"
	"strings"
)

func Path(parts ...string) string {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		panic("Product source location unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	for _, part := range parts {
		if filepath.IsAbs(part) {
			panic("absolute Product source segment")
		}
	}
	value := filepath.Join(append([]string{root}, parts...)...)
	relative, err := filepath.Rel(root, value)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		panic("Product source path escaped its owner")
	}
	return value
}
