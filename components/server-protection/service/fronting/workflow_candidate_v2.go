package fronting

import (
	"errors"
	"time"
)

type renderedCandidateV2 struct {
	Revision              string
	SHA256                string
	Bytes                 []byte
	CanonicalInput        []byte
	Listener              protectionListenerV2
	SelectorSetRevision   string
	MapRevision           string
	UpstreamIDSetRevision string
}

func renderWorkflowCandidateV2(current revalidatedPlanV2, checkpoint CheckpointV2, now time.Time) (renderedCandidateV2, error) {
	switch current.Plan.Strategy.Selected {
	case StrategyL4OneToOne:
		reference := current.Plan.Targets.BackendReferences[0]
		fact, ok := current.Facts[reference.CanonicalReferenceRevision]
		if !ok {
			return renderedCandidateV2{}, errors.New("backend_reference_stale")
		}
		candidate, err := RenderFixedL4CandidateV2(current.Plan, fact, now)
		if err != nil {
			return renderedCandidateV2{}, err
		}
		return renderedCandidateV2{Revision: candidate.Revision, SHA256: candidate.SHA256, Bytes: candidate.Bytes,
			CanonicalInput: candidate.CanonicalInput, Listener: candidate.Listener}, nil
	case StrategySNIPreread:
		authorities, err := authorityRevisionsV2(checkpoint)
		if err != nil {
			return renderedCandidateV2{}, err
		}
		candidate, err := RenderSNIPrereadCandidateV2(current.Plan, current.Input, authorities, now)
		if err != nil {
			return renderedCandidateV2{}, err
		}
		return renderedCandidateV2{Revision: candidate.Revision, SHA256: candidate.SHA256, Bytes: candidate.Bytes,
			CanonicalInput: candidate.CanonicalInput, Listener: candidate.Listener, SelectorSetRevision: candidate.SelectorSetRevision,
			MapRevision: candidate.MapRevision, UpstreamIDSetRevision: candidate.UpstreamIDSetRevision}, nil
	default:
		return renderedCandidateV2{}, errors.New("candidate_invalid")
	}
}

func validateWorkflowPlanShapeV2(plan FrontingStrategyPlanV2, now time.Time) error {
	switch plan.Strategy.Selected {
	case StrategyL4OneToOne:
		return validateFixedL4PlanShapeV2(plan, now)
	case StrategySNIPreread:
		return validateSNIPlanShapeV2(plan, now)
	default:
		return errors.New("candidate_invalid")
	}
}
