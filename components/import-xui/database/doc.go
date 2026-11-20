//go:build !minimal

// Package importxui imports a compatible panel SQLite database into the active s-ui
// database using the pinned Import source module and the existing destination
// resource owners.
//
// The module owns read-only source access, dialect mapping and preview types.
// This adapter supplies destination facts and coordinates transactional apply,
// dry-run and rollback using the Backend database lifecycle.
package importxui
