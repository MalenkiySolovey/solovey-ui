package failover

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"gorm.io/gorm"
)

type GroupReader interface {
	GroupNow(groupTag string) (string, bool)
}

type MemberStatus struct {
	Tag      string `json:"tag"`
	Healthy  bool   `json:"healthy"`
	Priority int    `json:"priority"`
}

type StatusEntry struct {
	Tag     string         `json:"tag"`
	Active  string         `json:"active"`
	AllDown bool           `json:"allDown"`
	Members []MemberStatus `json:"members"`
}

func Status(db *gorm.DB, core GroupReader) ([]StatusEntry, error) {
	groups, err := entityoutbounds.LoadFailoverGroups(db)
	if err != nil {
		return nil, err
	}
	result := make([]StatusEntry, 0, len(groups))
	for _, group := range groups {
		entry := StatusEntry{Tag: group.Tag, Members: make([]MemberStatus, 0, len(group.Members))}
		if core != nil {
			entry.Active, _ = core.GroupNow(group.Tag)
		}
		states, err := ReadMemberStates(db, group.Tag)
		if err != nil {
			return nil, err
		}
		byTag := make(map[string]model.FailoverMemberState, len(states))
		for _, state := range states {
			byTag[state.MemberTag] = state
		}
		anyHealthy := false
		for priority, member := range group.Members {
			healthy := byTag[member].Healthy
			entry.Members = append(entry.Members, MemberStatus{Tag: member, Healthy: healthy, Priority: priority})
			anyHealthy = anyHealthy || healthy
		}
		entry.AllDown = !anyHealthy
		result = append(result, entry)
	}
	return result, nil
}
