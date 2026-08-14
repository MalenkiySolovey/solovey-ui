//go:build !minimal

package cmd

// Full-profile CLI builds require the manifest-generated optional command
// composition. This prevents a silently incomplete release binary.
var _ = soloveyGeneratedOptionalCommandImports
