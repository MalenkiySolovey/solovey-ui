//go:build linux

package registry

// Official resolved has a real constructor only on Linux in pinned 1.13.14.
const supportsResolved = true
