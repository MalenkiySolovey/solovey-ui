package ipmonitor

import "time"

func Record(clientName, ip string) {
	if clientName == "" || ip == "" {
		return
	}
	revision := policyRevision()
	ipHash, display, ok := recordIPFields(ip)
	if !ok {
		return
	}
	allowCache.Lock()
	defer allowCache.Unlock()
	if revision != allowCache.revision {
		return
	}
	pending.Lock()
	defer pending.Unlock()
	recordLocked(clientName, ipHash, display, time.Now().Unix())
}

// Caller holds allowCache -> pending; privacy fields are already prepared.
func recordLocked(clientName, ipHash string, display *string, now int64) {
	if pending.byClient[clientName] == nil {
		pending.byClient[clientName] = map[string]pendingIP{}
	}
	pending.byClient[clientName][ipHash] = pendingIP{lastSeen: now, display: display}
	cacheAddIPLocked(clientName, ipHash)
}
