package decision

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
)

type Capabilities struct {
	RateLimit         bool
	SafePublicSurface bool
	HardBlock         bool
	AdvancedAccepted  bool
}

type Context struct {
	Enabled         bool
	ResourceStale   bool
	Whitelisted     bool
	Threshold       int
	RequestedAction domain.DecisionAction
	Capabilities    Capabilities
	Now             time.Time
}

type Decision struct {
	ResourceID string                `json:"resourceId"`
	Source     string                `json:"source"`
	Score      int                   `json:"score"`
	Action     domain.DecisionAction `json:"action"`
	Support    domain.SupportState   `json:"support"`
	ExpiresAt  *time.Time            `json:"expiresAt,omitempty"`
	Reasons    []scoring.ScoreReason `json:"reasons"`
	Warnings   []string              `json:"warnings,omitempty"`
}

func Decide(state scoring.ScoreState, ctx Context) Decision {
	result := Decision{
		ResourceID: state.ResourceID,
		Source:     state.SourcePrefix.String(),
		Score:      state.CurrentScore,
		Action:     domain.DecisionRecordOnly,
		Support:    domain.SupportSupported,
		ExpiresAt:  state.ExpiresAt,
		Reasons:    append([]scoring.ScoreReason(nil), state.Reasons...),
	}
	if !ctx.Enabled {
		result.Warnings = append(result.Warnings, "profile_disabled")
		return result
	}
	if ctx.Whitelisted {
		result.ExpiresAt = nil
		result.Warnings = append(result.Warnings, "source_allowlisted")
		return result
	}
	if ctx.ResourceStale {
		result.Support = domain.SupportDegraded
		result.Warnings = append(result.Warnings, "resource_drift")
		return result
	}
	if ctx.Threshold <= 0 {
		ctx.Threshold = domain.DefaultScoreThreshold
	}
	if state.CurrentScore < ctx.Threshold {
		return result
	}
	requested := ctx.RequestedAction
	if requested == "" {
		requested = domain.DecisionRecordOnly
	}
	switch requested {
	case domain.DecisionRecordOnly:
		return result
	case domain.DecisionRateLimit:
		if ctx.Capabilities.RateLimit {
			result.Action = domain.DecisionRateLimit
			return result
		}
	case domain.DecisionRouteToDecoy:
		if ctx.Capabilities.SafePublicSurface {
			result.Action = domain.DecisionRouteToDecoy
			return result
		}
	case domain.DecisionBlock:
		if ctx.Capabilities.HardBlock && ctx.Capabilities.AdvancedAccepted {
			result.Action = domain.DecisionBlock
			return result
		}
	default:
		result.Support = domain.SupportUnsupported
		result.Warnings = append(result.Warnings, "unsupported_action")
		return result
	}
	result.Support = domain.SupportMissingCapability
	result.Warnings = append(result.Warnings, "missing_capability")
	return result
}
