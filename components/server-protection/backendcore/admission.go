// Package backendcore binds protection consumers to the existing Core owner.
package backendcore

import (
	corecontract "github.com/MalenkiySolovey/solovey-host/coreruntime/inboundcontrol"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

type Admission struct{}

func (Admission) VerifyRuntimeIdentity(identity corecontract.CoreRuntimeIdentityV1) bool {
	return coreinboundcontrol.RuntimeIdentityVerifiedV1(identity)
}

func (Admission) CapabilityResolverRevision() string {
	return coreinboundcontrol.CapabilityResolverRevisionV1
}
