// Package durable preserves the Product import path for shared owner capabilities.
package durable

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/durable"

type Storage = canonical.Storage
type Scope = canonical.Scope
type Outcome = canonical.Outcome
type Result = canonical.Result
type Failure = canonical.Failure
type AuthorityStatus = canonical.AuthorityStatus
type Migration = canonical.Migration
type Restore = canonical.Restore
type Inspection = canonical.Inspection
type DataDrop = canonical.DataDrop
type Lifecycle = canonical.Lifecycle

const (
	Live      = canonical.Live
	Candidate = canonical.Candidate
	Verified  = canonical.Verified
	Rejected  = canonical.Rejected
	Unknown   = canonical.Unknown
)
