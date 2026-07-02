//go:build !minimal

package app

// Full-profile builds must generate app/components_generated.go from component
// manifests before compiling. This file keeps the composition root generic, so
// adding an optional component does not require editing core application code.
var _ = soloveyGeneratedComponentImports
