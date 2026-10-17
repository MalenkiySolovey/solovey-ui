package state

import (
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

// LifecycleObservation is the last lifecycle call result in this process. It is
// not a health/readiness assertion. InstanceRetention refers only to the
// supervisor's runtime instance, never to external resources owned by a feature.
type LifecycleObservation struct {
	Phase             string     `json:"phase"`
	InstanceRetention string     `json:"instanceRetention"`
	Source            string     `json:"source"`
	ObservedAt        *time.Time `json:"observedAt,omitempty"`
	ReasonCode        string     `json:"reasonCode"`
}

const (
	LifecycleStarted      = "STARTED"
	LifecycleStartFailed  = "START_FAILED"
	LifecycleStopped      = "STOPPED"
	LifecycleStopFailed   = "STOP_FAILED"
	LifecycleNotObserved  = "NOT_OBSERVED"
	LifecycleBinaryAbsent = "BINARY_ABSENT"
	LifecycleNotInstalled = "NOT_INSTALLED"
	InstanceRetained      = "RETAINED"
	InstanceReleased      = "RELEASED"
	InstanceUnknown       = "UNKNOWN"
)

var lifecycleResults = struct {
	sync.RWMutex
	epoch   uint64
	results map[string]LifecycleObservation
}{}

// LifecyclePublisher belongs to one supervisor incarnation. New construction
// starts a new process-local observation epoch; older publishers cannot replace
// its results after a late lifecycle call completes.
type LifecyclePublisher struct{ epoch uint64 }

func NewLifecyclePublisher() LifecyclePublisher {
	lifecycleResults.Lock()
	defer lifecycleResults.Unlock()
	lifecycleResults.epoch++
	lifecycleResults.results = make(map[string]LifecycleObservation)
	return LifecyclePublisher{epoch: lifecycleResults.epoch}
}

func (p LifecyclePublisher) RecordStart(id string, succeeded bool) {
	phase, retention, reason := LifecycleStartFailed, InstanceUnknown, "START_FAILED"
	if succeeded {
		phase, retention, reason = LifecycleStarted, InstanceRetained, "START_RETURNED"
	}
	p.record(id, phase, retention, reason)
}

func (p LifecyclePublisher) RecordStop(id string, succeeded bool) {
	phase, retention, reason := LifecycleStopFailed, InstanceRetained, "STOP_FAILED"
	if succeeded {
		phase, retention, reason = LifecycleStopped, InstanceReleased, "STOP_RETURNED"
	}
	p.record(id, phase, retention, reason)
}

func (p LifecyclePublisher) record(id, phase, retention, reason string) {
	if manifest.ValidateID(id) != nil {
		return
	}
	lifecycleResults.Lock()
	defer lifecycleResults.Unlock()
	if p.epoch == 0 || p.epoch != lifecycleResults.epoch {
		return
	}
	if _, exists := lifecycleResults.results[id]; !exists && len(lifecycleResults.results) >= installstate.MaxInstalledComponents {
		return
	}
	now := time.Now().UTC()
	lifecycleResults.results[id] = LifecycleObservation{
		Phase: phase, InstanceRetention: retention, Source: "componenthost.supervisor", ObservedAt: &now, ReasonCode: reason,
	}
}

func LifecycleForComponent(id string, installed, availableInBinary bool) LifecycleObservation {
	lifecycleResults.RLock()
	result, exists := lifecycleResults.results[id]
	lifecycleResults.RUnlock()
	if exists {
		// Eligibility changes must not hide a failed Stop or any other actual
		// lifecycle result. Copy the timestamp so callers cannot rewrite it.
		if result.ObservedAt != nil {
			observedAt := *result.ObservedAt
			result.ObservedAt = &observedAt
		}
		return result
	}
	result = LifecycleObservation{Phase: LifecycleNotObserved, InstanceRetention: InstanceUnknown,
		Source: "componenthost.supervisor", ReasonCode: "NO_LIFECYCLE_RESULT"}
	if !installed {
		result.Phase, result.Source, result.ReasonCode = LifecycleNotInstalled, "componenthost.installstate", "OWNER_NOT_INSTALLED"
	} else if !availableInBinary {
		result.Phase, result.Source, result.ReasonCode = LifecycleBinaryAbsent, "componenthost.registry", "OWNER_BINARY_ABSENT"
	}
	return result
}
