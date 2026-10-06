//go:build linux

package registry

// The pinned official resolved transport has a real constructor only on Linux.
const supportsResolved = true
