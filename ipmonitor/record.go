package ipmonitor

import "time"

func Record(clientName, ip string) {
	if clientName == "" || ip == "" {
		return
	}
	ipHash, display, ok := recordIPFields(ip)
	if !ok {
		return
	}
	now := time.Now().Unix()
	pending.Lock()
	if pending.byClient[clientName] == nil {
		pending.byClient[clientName] = map[string]pendingIP{}
	}
	pending.byClient[clientName][ipHash] = pendingIP{lastSeen: now, display: display}
	pending.Unlock()
	cacheAddIP(clientName, ipHash)
}
