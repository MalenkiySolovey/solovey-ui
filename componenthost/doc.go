// Package componenthost owns the in-process component host seam.
//
// Core code discovers components through the registry and passes explicit host
// dependencies through Deps. Components must not be imported directly outside
// the profile aggregators in app/components_*.go.
package componenthost
