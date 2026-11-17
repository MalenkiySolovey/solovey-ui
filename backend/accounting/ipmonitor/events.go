package ipmonitor

import (
	"errors"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/realtime"
)

type Hooks struct {
	SecurityEventAudit func(clientName, kind string, payload map[string]any)
	Warning            func(...any)
}

var securityEventAudit = struct {
	sync.RWMutex
	next    uint64
	id      uint64
	hook    func(clientName, kind string, payload map[string]any)
	warning func(...any)
}{}

// RegisterHooks installs the single composition-owned durable
// audit sink and warning logger, returning an idempotent lifecycle cleanup.
func RegisterHooks(hooks Hooks) (func(), error) {
	if hooks.SecurityEventAudit == nil {
		return nil, errors.New("security event audit hook is nil")
	}
	securityEventAudit.Lock()
	defer securityEventAudit.Unlock()
	if securityEventAudit.hook != nil {
		return nil, errors.New("security event audit hook already registered")
	}
	securityEventAudit.next++
	id := securityEventAudit.next
	securityEventAudit.id = id
	securityEventAudit.hook = hooks.SecurityEventAudit
	securityEventAudit.warning = hooks.Warning
	var once sync.Once
	return func() {
		once.Do(func() {
			securityEventAudit.Lock()
			if securityEventAudit.id == id {
				securityEventAudit.id = 0
				securityEventAudit.hook = nil
				securityEventAudit.warning = nil
			}
			securityEventAudit.Unlock()
		})
	}, nil
}

func currentSecurityEventAuditHook() func(string, string, map[string]any) {
	securityEventAudit.RLock()
	defer securityEventAudit.RUnlock()
	return securityEventAudit.hook
}

func publishSecurityEvent(clientName, kind string, payload map[string]any) {
	if !shouldPublishSecurityEvent(clientName, kind, time.Now()) {
		return
	}
	realtime.Publish(realtime.TopicSecurityEvent, payload)
	if hook := currentSecurityEventAuditHook(); hook != nil {
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

func currentWarningHook() func(...any) {
	securityEventAudit.RLock()
	defer securityEventAudit.RUnlock()
	return securityEventAudit.warning
}

// RegisterSecurityEventAuditHook installs an audit sink with the same single lifecycle owner.
func RegisterSecurityEventAuditHook(hook func(string, string, map[string]any)) (func(), error) {
	return RegisterHooks(Hooks{SecurityEventAudit: hook})
}
