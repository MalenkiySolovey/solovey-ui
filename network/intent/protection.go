// Package intent preserves typed network requests through the shared Host
// vocabulary. Allocation, accepted plans and execution remain with Planner.
package intent

import (
	canonical "github.com/MalenkiySolovey/solovey-host/network/intent"
)

const ProtectionAllocation = canonical.ProtectionAllocation

const ProtectionFamily = canonical.ProtectionFamily

const ProtectionTable = canonical.ProtectionTable

const ProtectionTableCLI = canonical.ProtectionTableCLI

const ProtectionResourceRef = canonical.ProtectionResourceRef

const ProtectionEndpointMode = canonical.ProtectionEndpointMode

type ResourceBinding = canonical.ResourceBinding

type Intent = canonical.Intent

type SetClass = canonical.SetClass

const GraySet = canonical.GraySet

const RateSet = canonical.RateSet

const QuarantineSet = canonical.QuarantineSet

const BlockSet = canonical.BlockSet

type TimedSource = canonical.TimedSource

type ProtectionLimits = canonical.ProtectionLimits

type PacketLimit = canonical.PacketLimit

type ProtectionRates = canonical.ProtectionRates

type ProtectionEndpoint = canonical.ProtectionEndpoint

type ProtectionExemption = canonical.ProtectionExemption

type ProtectionEnforcementIntent = canonical.ProtectionEnforcementIntent
