package firewall

import (
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// Raw negative fixtures write the Backend schema model so ORM metadata stays
// with the same concrete owner even when the observed value is a feature DTO.
func backendCompositionFixture(value durable.FirewallCompositionModel) repository.FirewallCompositionModel {
	return repository.FirewallCompositionModel{
		Runtime: repository.FirewallRuntimeBinding(value.Runtime), ID: value.ID, Schema: value.Schema,
		Revision: value.Revision, ManagedPlanRevision: value.ManagedPlanRevision, CandidateSHA256: value.CandidateSHA256,
		CandidateSemanticSHA256: value.CandidateSemanticSHA256, CandidateTimedMembershipSHA256: value.CandidateTimedMembershipSHA256,
		BindingsJSON: value.BindingsJSON, State: value.State, AppliedOperationID: value.AppliedOperationID, UpdatedAt: value.UpdatedAt,
	}
}
