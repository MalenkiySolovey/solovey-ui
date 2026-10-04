package ipmonitor

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	logruntime "github.com/MalenkiySolovey/solovey-ui/logger"
)

var loadErrLog = struct {
	sync.Mutex
	last time.Time
}{}

// Tests coordinate the persistence boundary with invalidation without sleeps.
var loadPolicyForAdmission = loadCacheEntry

// Allow queries policy without observing a source. Runtime admission must use
// ObserveAndAllow so checking the last free slot and reserving it are atomic.
func Allow(clientName, ip string) bool { return admit(clientName, ip, false) }

// ObserveAndAllow records an accepted source exactly once before a competing
// admission can consume its slot. No rejected source enters pending/history.
func ObserveAndAllow(clientName, ip string) bool { return admit(clientName, ip, true) }

func policyRevision() uint64 {
	allowCache.Lock()
	defer allowCache.Unlock()
	return allowCache.revision
}

func admit(clientName, ip string, observe bool) bool {
	if clientName == "" || ip == "" {
		return true
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return false
	}
	revision := policyRevision()
	var ipHash string
	var display *string
	var ok bool
	if observe {
		ipHash, display, ok = recordIPFields(ip)
	} else {
		var err error
		ipHash, err = hashIP(ip)
		ok = err == nil
	}
	if !ok || !ensureClientPolicy(clientName, revision) {
		return false
	}
	return admitPrepared(clientName, ipHash, display, observe, revision)
}

func admitPrepared(clientName, ipHash string, display *string, observe bool, revision uint64) bool {
	// Validate the revision at the decision, including cache hits. DB/privacy
	// work and event callbacks never run under either state lock.
	allowCache.Lock()
	entry, loaded := allowCache.byClient[clientName]
	if revision != allowCache.revision || !loaded || !time.Now().Before(entry.expiresAt) {
		allowCache.Unlock()
		return false
	}
	pending.Lock()
	count := 0
	allowed := true
	if entry.mode == ModeEnforce && entry.limit > 0 {
		seen := make(map[string]struct{}, len(entry.ips)+len(pending.byClient[clientName]))
		for hash := range entry.ips {
			seen[hash] = struct{}{}
		}
		for hash := range pending.byClient[clientName] {
			seen[hash] = struct{}{}
		}
		for batch := range pending.flushing {
			for hash := range batch.snapshot[clientName] {
				seen[hash] = struct{}{}
			}
		}
		_, known := seen[ipHash]
		seen[ipHash] = struct{}{}
		count = len(seen)
		allowed = known || count <= entry.limit
	}
	if allowed && observe {
		recordLocked(clientName, ipHash, display, time.Now().Unix())
	}
	pending.Unlock()
	allowCache.Unlock()
	if !allowed {
		publishSecurityEvent(clientName, "ip_enforced_reject", map[string]any{
			"kind": "ip_enforced_reject", "client": clientName,
			"ipHash": ipHash, "limit": entry.limit, "count": count,
		})
	}
	return allowed
}

// Collapse overlapping misses for a client/revision. A late follower checks
// the cache inside the flight before I/O. Expired policy is not trusted on
// failure; a later request may retry. No stale fallback crosses invalidation.
func ensureClientPolicy(clientName string, revision uint64) bool {
	allowCache.Lock()
	if revision != allowCache.revision {
		allowCache.Unlock()
		return false
	}
	entry, ok := allowCache.byClient[clientName]
	fresh := ok && time.Now().Before(entry.expiresAt)
	allowCache.Unlock()
	if fresh {
		return true
	}
	result, _, _ := allowCacheRefresh.Do(fmt.Sprintf("%d:%s", revision, clientName), func() (any, error) {
		allowCache.Lock()
		if revision != allowCache.revision {
			allowCache.Unlock()
			return false, nil
		}
		entry, ok := allowCache.byClient[clientName]
		if ok && time.Now().Before(entry.expiresAt) {
			allowCache.Unlock()
			return true, nil
		}
		delete(allowCache.byClient, clientName)
		allowCache.Unlock()

		entry, ok = loadPolicyForAdmission(clientName, time.Now())
		allowCache.Lock()
		defer allowCache.Unlock()
		if revision != allowCache.revision || !ok {
			return false, nil
		}
		entry.expiresAt = time.Now().Add(allowCacheTTL)
		allowCache.byClient[clientName] = entry
		return true, nil
	})
	return result == true
}

func WarmUp() error {
	revision := policyRevision()
	entries, err := loadWarmUpEntries(time.Now())
	if err != nil {
		return err
	}
	allowCache.Lock()
	defer allowCache.Unlock()
	if revision != allowCache.revision {
		return errors.New("ipmonitor policy invalidated during warmup")
	}
	// Warmup replaces policy knowledge; pre-warmup loads/decisions are fenced.
	// Pending and transaction-owned flush observations remain visible.
	allowCache.revision++
	allowCache.byClient = entries
	return nil
}

func cachedClient(clientName string, now time.Time) (allowCacheEntry, bool) {
	allowCache.Lock()
	defer allowCache.Unlock()
	if entry, ok := allowCache.byClient[clientName]; ok && now.Before(entry.expiresAt) {
		return cloneCacheEntry(entry), true
	}
	return allowCacheEntry{}, false
}

func logLoadCacheError(context string, err error) {
	loadErrLog.Lock()
	defer loadErrLog.Unlock()
	if !loadErrLog.last.IsZero() && time.Since(loadErrLog.last) < 30*time.Second {
		return
	}
	loadErrLog.last = time.Now()
	logruntime.Warning("ipmonitor: ip-limit ", context, " lookup failed; failing closed: ", err)
}

func cloneCacheEntry(entry allowCacheEntry) allowCacheEntry {
	clone := allowCacheEntry{limit: entry.limit, mode: entry.mode, ips: make(map[string]struct{}, len(entry.ips)), expiresAt: entry.expiresAt}
	for ip := range entry.ips {
		clone.ips[ip] = struct{}{}
	}
	return clone
}

// Caller holds allowCache. Pending is acquired only after allowCache.
func cacheAddIPLocked(clientName, ip string) {
	entry, ok := allowCache.byClient[clientName]
	if !ok {
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
	allowCache.revision++
	delete(allowCache.byClient, clientName)
}

// InvalidateAllCache retains accepted observations while discarding policy.
// ResetCaches additionally drops observation/privacy state for DB replacement.
func InvalidateAllCache() {
	allowCache.Lock()
	defer allowCache.Unlock()
	allowCache.revision++
	allowCache.byClient = map[string]allowCacheEntry{}
}
