package ipmonitor

import (
	"sync"
	"time"

	logruntime "github.com/MalenkiySolovey/solovey-ui/logger"
)

var loadErrLog = struct {
	sync.Mutex
	last time.Time
}{}

func Allow(clientName, ip string) bool {
	if clientName == "" || ip == "" {
		return true
	}
	ipHash, err := hashIP(ip)
	if err != nil {
		return true
	}
	entry, ok := cachedClient(clientName, time.Now())
	if !ok {
		refreshClientAsync(clientName)
		return true
	}
	if entry.mode != ModeEnforce || entry.limit <= 0 {
		return true
	}
	seen := map[string]struct{}{ipHash: {}}
	for seenHash := range entry.ips {
		seen[seenHash] = struct{}{}
	}
	pending.Lock()
	for seenHash := range pending.byClient[clientName] {
		seen[seenHash] = struct{}{}
	}
	pending.Unlock()
	if len(seen) <= entry.limit {
		return true
	}
	publishSecurityEvent(clientName, "ip_enforced_reject", map[string]any{
		"kind": "ip_enforced_reject", "client": clientName,
		"ipHash": ipHash, "limit": entry.limit, "count": len(seen),
	})
	return false
}

func WarmUp() error {
	entries, err := loadWarmUpEntries(time.Now())
	if err != nil {
		return err
	}
	allowCache.Lock()
	allowCache.byClient = entries
	allowCache.Unlock()
	return nil
}

func cachedClient(clientName string, now time.Time) (allowCacheEntry, bool) {
	allowCache.Lock()
	defer allowCache.Unlock()
	if entry, ok := allowCache.byClient[clientName]; ok && now.Before(entry.expiresAt) {
		return cloneCacheEntry(entry), true
	}
	delete(allowCache.byClient, clientName)
	return allowCacheEntry{}, false
}

func logLoadCacheError(context string, err error) {
	loadErrLog.Lock()
	defer loadErrLog.Unlock()
	if !loadErrLog.last.IsZero() && time.Since(loadErrLog.last) < 30*time.Second {
		return
	}
	loadErrLog.last = time.Now()
	logruntime.Warning("ipmonitor: ip-limit ", context, " lookup failed; failing open (allowing): ", err)
}

func refreshClientAsync(clientName string) {
	allowCacheRefresh.Lock()
	if _, ok := allowCacheRefresh.inFlight[clientName]; ok {
		allowCacheRefresh.Unlock()
		return
	}
	allowCacheRefresh.inFlight[clientName] = struct{}{}
	allowCacheRefresh.Unlock()
	go func() {
		defer func() {
			allowCacheRefresh.Lock()
			delete(allowCacheRefresh.inFlight, clientName)
			allowCacheRefresh.Unlock()
		}()
		refreshClient(clientName, time.Now())
	}()
}

func refreshClient(clientName string, now time.Time) bool {
	entry, ok := loadCacheEntry(clientName, now)
	allowCache.Lock()
	defer allowCache.Unlock()
	if !ok {
		delete(allowCache.byClient, clientName)
		return false
	}
	allowCache.byClient[clientName] = entry
	return true
}

func cloneCacheEntry(entry allowCacheEntry) allowCacheEntry {
	clone := allowCacheEntry{limit: entry.limit, mode: entry.mode, ips: make(map[string]struct{}, len(entry.ips)), expiresAt: entry.expiresAt}
	for ip := range entry.ips {
		clone.ips[ip] = struct{}{}
	}
	return clone
}

func cacheAddIP(clientName, ip string) {
	allowCache.Lock()
	defer allowCache.Unlock()
	entry, ok := allowCache.byClient[clientName]
	if !ok || time.Now().After(entry.expiresAt) {
		return
	}
	if entry.ips == nil {
		entry.ips = map[string]struct{}{}
	}
	entry.ips[ip] = struct{}{}
	allowCache.byClient[clientName] = entry
}

func invalidateCache(clientName string) {
	allowCache.Lock()
	defer allowCache.Unlock()
	delete(allowCache.byClient, clientName)
}

func InvalidateAllCache() {
	allowCache.Lock()
	defer allowCache.Unlock()
	allowCache.byClient = map[string]allowCacheEntry{}
}
