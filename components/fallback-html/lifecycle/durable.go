//go:build !minimal

// Package lifecycle delegates portable owner sequencing to its source module.
package lifecycle

import feature "github.com/MalenkiySolovey/solovey-component-fallback-html/lifecycle"

type Owner = feature.Owner
