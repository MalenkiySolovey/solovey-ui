package firewall

import (
	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
	contract "github.com/MalenkiySolovey/solovey-host/network/planning"
)

// NetworkPlanner is the existing coordinator. The feature supplies typed
// intent, while allocation, accepted candidate receipts and comparison remain
// behind this owner boundary.
type NetworkPlanner interface {
	CompileProtection(networkintent.ProtectionEnforcementIntent, contract.Facts) (contract.ProtectionPlan, error)
	Accept(contract.ProtectionPlan, contract.Facts, string) (contract.Accepted, error)
	CheckAccepted(contract.Accepted, contract.Facts, string) error
	CompareResource(contract.Ownership, string, bool, []contract.Allocation) []contract.Drift
	CheckRollback(contract.RollbackIntent) error
}
