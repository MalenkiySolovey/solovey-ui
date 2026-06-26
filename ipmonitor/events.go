package ipmonitor

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/realtime"
)

// SecurityEventAuditHook mirrors debounced enforcement events into durable audit storage.
var SecurityEventAuditHook func(clientName, kind string, payload map[string]any)

func publishSecurityEvent(clientName, kind string, payload map[string]any) {
	if !shouldPublishSecurityEvent(clientName, kind, time.Now()) {
		return
	}
	realtime.Publish(realtime.TopicSecurityEvent, payload)
	if hook := SecurityEventAuditHook; hook != nil {
		hook(clientName, kind, payload)
	}
}

func shouldPublishSecurityEvent(clientName, kind string, now time.Time) bool {
	key := clientName + "|" + kind
	securityEvents.Lock()
	defer securityEvents.Unlock()
	if last, ok := securityEvents.lastEmittedAt[key]; ok && now.Sub(last) < securityEventDebounce {
		return false
	}
	securityEvents.lastEmittedAt[key] = now
	for eventKey, last := range securityEvents.lastEmittedAt {
		if now.Sub(last) > securityEventMaxMapAge {
			delete(securityEvents.lastEmittedAt, eventKey)
		}
	}
	return true
}
