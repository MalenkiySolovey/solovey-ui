// Package rulepolicy owns the shared product budget for recursive core rules.
package rulepolicy

const (
	MaxDepth = 64
	MaxNodes = 4096 // Per DNS/route tree, including roots.
)
