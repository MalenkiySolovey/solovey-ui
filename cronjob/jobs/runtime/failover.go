package runtime

import (
	"context"
	"sync"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/service/failover"
)

const failoverProbeConcurrency = 4

type failoverGroupState struct {
	lastProbe   time.Time
	lastAllDown bool
	health      map[string]failover.MemberHealth
}

type FailoverJob struct {
	service.ConfigService

	mu     sync.Mutex
	states map[string]*failoverGroupState
	now    func() time.Time

	probe        func(context.Context, string, string) bool
	switchMember func(string, string) error
	activeMember func(string) (string, bool)
}

func NewFailoverJob() *FailoverJob {
	return &FailoverJob{states: make(map[string]*failoverGroupState), now: time.Now}
}

func (j *FailoverJob) Run() {
	db := dbsqlite.DB()
	if db == nil {
		return
	}
	groups, err := entityoutbounds.LoadFailoverGroups(db)
	if err != nil {
		logger.Warning("failover: load groups: ", err)
		return
	}
	if len(groups) == 0 {
		return
	}
	core := service.DefaultRuntime().Core()
	if core == nil || !core.IsRunning() {
		return
	}
	directTag := entityoutbounds.DirectFallbackTag(db)
	for _, group := range groups {
		j.runGroup(core, group, directTag)
	}
}

func (j *FailoverJob) runGroup(core *coreruntime.Core, group entityoutbounds.FailoverGroup, directTag string) {
	if !group.Enabled || len(group.Members) == 0 {
		return
	}

	j.mu.Lock()
	state := j.states[group.Tag]
	if state == nil {
		state = &failoverGroupState{health: make(map[string]failover.MemberHealth)}
		j.states[group.Tag] = state
	}
	due := state.lastProbe.IsZero() || j.now().Sub(state.lastProbe) >= group.Interval
	j.mu.Unlock()
	if !due {
		return
	}

	results := j.probeMembers(group)
	j.mu.Lock()
	for _, member := range group.Members {
		health := state.health[member]
		if results[member] {
			health.ConsecutiveUp++
			health.ConsecutiveDown = 0
		} else {
			health.ConsecutiveDown++
			health.ConsecutiveUp = 0
		}
		state.health[member] = health
	}
	state.lastProbe = j.now()
	snapshot := make(map[string]failover.MemberHealth, len(state.health))
	for tag, health := range state.health {
		snapshot[tag] = health
	}
	j.mu.Unlock()

	j.persistState(group, snapshot)
	current, _ := j.active(core, group.Tag)
	fallback := ""
	if directTag != "" && !memberListContains(group.Members, directTag) {
		fallback = directTag
	}
	decision := failover.SelectMember(failover.DecisionInput{
		Members:        group.Members,
		Health:         snapshot,
		Current:        current,
		Hysteresis:     group.Hysteresis,
		DirectFallback: fallback,
	})
	active := current
	if decision.ShouldSwitch {
		if err := j.switchTo(core, group.Tag, decision.Target); err != nil {
			logger.Warning("failover: switch ", group.Tag, " -> ", decision.Target, ": ", err)
			j.publishLiveStatus(group, snapshot, active, decision.AllDown)
			return
		}
		active = decision.Target
		logger.Info("failover: group ", group.Tag, " -> ", decision.Target, " (", decision.Reason, ")")
	}
	j.publishLiveStatus(group, snapshot, active, decision.AllDown)
	if j.recordAllDownEdge(group.Tag, decision.AllDown) {
		j.alertAllDown(group)
	}
}

func (j *FailoverJob) recordAllDownEdge(groupTag string, allDown bool) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	state := j.states[groupTag]
	if state == nil {
		state = &failoverGroupState{health: make(map[string]failover.MemberHealth)}
		j.states[groupTag] = state
	}
	wasAllDown := state.lastAllDown
	state.lastAllDown = allDown
	return allDown && !wasAllDown
}

func (j *FailoverJob) publishLiveStatus(group entityoutbounds.FailoverGroup, snapshot map[string]failover.MemberHealth, active string, allDown bool) {
	entry := failover.StatusEntry{
		Tag:     group.Tag,
		Active:  active,
		AllDown: allDown,
		Members: make([]failover.MemberStatus, 0, len(group.Members)),
	}
	for priority, member := range group.Members {
		entry.Members = append(entry.Members, failover.MemberStatus{
			Tag:      member,
			Healthy:  snapshot[member].ConsecutiveUp >= 1,
			Priority: priority,
		})
	}
	realtime.Publish(realtime.TopicFailoverStatus, entry)
}

func (j *FailoverJob) alertAllDown(group entityoutbounds.FailoverGroup) {
	logger.Warning("failover: all members down for group ", group.Tag)
	if err := (&service.AuditService{}).Record(service.AuditEvent{
		Actor:    "system",
		Event:    "failover_all_down",
		Resource: "outbound",
		Severity: service.AuditSeverityWarn,
		Details:  map[string]any{"group": group.Tag, "members": group.Members},
	}); err != nil {
		logger.Warning("failover: all-down audit: ", err)
	}
	realtime.Publish(realtime.TopicCoreState, map[string]any{
		"warning": "failover_all_down",
		"group":   group.Tag,
	})
}

func (j *FailoverJob) probeMembers(group entityoutbounds.FailoverGroup) map[string]bool {
	results := make(map[string]bool, len(group.Members))
	var mu sync.Mutex
	var wait sync.WaitGroup
	sem := make(chan struct{}, failoverProbeConcurrency)
	ctx, cancel := context.WithTimeout(context.Background(), group.Interval)
	defer cancel()
	for _, member := range group.Members {
		wait.Add(1)
		go func(tag string) {
			defer wait.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var healthy bool
			if j.probe != nil {
				healthy = j.probe(ctx, tag, group.ProbeTarget)
			} else {
				healthy = j.ConfigService.CheckOutboundWithContext(ctx, tag, group.ProbeTarget).OK
			}
			mu.Lock()
			results[tag] = healthy
			mu.Unlock()
		}(member)
	}
	wait.Wait()
	return results
}

func (j *FailoverJob) active(core *coreruntime.Core, groupTag string) (string, bool) {
	if j.activeMember != nil {
		return j.activeMember(groupTag)
	}
	return core.GroupNow(groupTag)
}

func (j *FailoverJob) switchTo(core *coreruntime.Core, groupTag, memberTag string) error {
	if j.switchMember != nil {
		return j.switchMember(groupTag, memberTag)
	}
	return core.SelectGroupMember(groupTag, memberTag)
}

func (j *FailoverJob) persistState(group entityoutbounds.FailoverGroup, snapshot map[string]failover.MemberHealth) {
	states := make([]model.FailoverMemberState, 0, len(group.Members))
	lastProbeAt := j.now().Unix()
	for _, member := range group.Members {
		health := snapshot[member]
		states = append(states, model.FailoverMemberState{
			GroupTag: group.Tag, MemberTag: member,
			Healthy: health.ConsecutiveUp >= 1, ConsecUp: health.ConsecutiveUp,
			ConsecDown: health.ConsecutiveDown, LastProbeAt: lastProbeAt,
		})
	}
	if err := failover.WriteMemberStates(dbsqlite.DB(), states); err != nil {
		logger.Warning("failover: persist state: ", err)
	}
}

func memberListContains(members []string, tag string) bool {
	for _, member := range members {
		if member == tag {
			return true
		}
	}
	return false
}
