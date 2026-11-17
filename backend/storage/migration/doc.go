// Package migration owns the Panel SQLite migration transaction, exact journal
// validation and version publication. Product composes its existing release
// policy and Host-owned migration steps into one ordered Plan. No connection is
// published as the live Panel database by this package.
package migration
