package failover

import entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"

type MemberHealth struct {
	ConsecutiveUp   int
	ConsecutiveDown int
}

type DecisionInput struct {
	Members        []string
	Health         map[string]MemberHealth
	Current        string
	Hysteresis     int
	DirectFallback string
}

type Decision struct {
	Target       string
	ShouldSwitch bool
	AllDown      bool
	Reason       string
}

// SelectMember is pure: policy can be tested without a database, clock, or core.
func SelectMember(input DecisionInput) Decision {
	if len(input.Members) == 0 {
		return Decision{Reason: "no_members"}
	}
	hysteresis := input.Hysteresis
	if hysteresis < 1 {
		hysteresis = entityoutbounds.DefaultHysteresis
	}
	indexOf := func(tag string) int {
		for index, member := range input.Members {
			if member == tag {
				return index
			}
		}
		return -1
	}
	isUp := func(tag string) bool { return input.Health[tag].ConsecutiveUp >= 1 }
	isConfirmed := func(tag string) bool { return input.Health[tag].ConsecutiveUp >= hysteresis }

	bestUp, bestConfirmed := "", ""
	for _, member := range input.Members {
		if bestUp == "" && isUp(member) {
			bestUp = member
		}
		if bestConfirmed == "" && isConfirmed(member) {
			bestConfirmed = member
		}
	}
	decide := func(target, reason string, allDown bool) Decision {
		return Decision{Target: target, ShouldSwitch: target != input.Current, AllDown: allDown, Reason: reason}
	}
	if bestUp == "" {
		if input.DirectFallback != "" {
			return decide(input.DirectFallback, "all_down_direct", true)
		}
		return decide(input.Members[0], "all_down_hold", true)
	}
	if input.Current == "" {
		return decide(bestUp, "priority", false)
	}
	currentIndex := indexOf(input.Current)
	if currentIndex < 0 {
		return decide(bestUp, "failback", false)
	}
	if !isUp(input.Current) {
		return decide(bestUp, "failover", false)
	}
	if bestConfirmed != "" && indexOf(bestConfirmed) < currentIndex {
		return decide(bestConfirmed, "failback", false)
	}
	return decide(input.Current, "sticky", false)
}
