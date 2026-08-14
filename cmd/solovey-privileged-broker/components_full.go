//go:build !minimal

package main

// Full-profile broker builds resolve optional semantic handlers through the
// manifest-generated composition seam. Missing generation must fail closed.
var _ = soloveyGeneratedPrivilegedBrokerImports
