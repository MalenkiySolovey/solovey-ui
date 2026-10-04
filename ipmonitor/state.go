package ipmonitor

import (
	"sync"
	"time"

	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"golang.org/x/sync/singleflight"
)

const (
	ModeMonitor = "monitor"
	ModeEnforce = "enforce"

	allowCacheTTL          = 30 * time.Second
	securityEventDebounce  = 60 * time.Second
	securityEventMaxMapAge = time.Hour
	ipMaskPrefix           = 12
)

type pendingIP struct {
	lastSeen int64
	display  *string
}

type allowCacheEntry struct {
	limit     int
	mode      string
	ips       map[string]struct{}
	expiresAt time.Time
}

var pending = struct {
	sync.Mutex
	byClient map[string]map[string]pendingIP
	flushing map[*FlushBatch]struct{}
}{byClient: map[string]map[string]pendingIP{}, flushing: map[*FlushBatch]struct{}{}}

var allowCache = struct {
	sync.Mutex
	byClient map[string]allowCacheEntry
	revision uint64
}{byClient: map[string]allowCacheEntry{}}

var allowCacheRefresh singleflight.Group

var securityEvents = struct {
	sync.Mutex
	lastEmittedAt map[string]time.Time
}{lastEmittedAt: map[string]time.Time{}}

var ipHashSalt = struct {
	sync.Mutex
	value []byte
}{}

var ipPrivacySettings = struct {
	sync.Mutex
	showRaw   bool
	expiresAt time.Time
}{}

func init() {
	dbhooks.RegisterResetHook("ipmonitor", ResetCaches)
}

func ResetCaches() {
	// Privacy and policy move to the new database together. Source preparation
	// never holds these mutexes while acquiring allowCache or pending.
	ipHashSalt.Lock()
	ipPrivacySettings.Lock()
	allowCache.Lock()
	pending.Lock()
	pending.byClient = map[string]map[string]pendingIP{}
	pending.flushing = map[*FlushBatch]struct{}{}
	allowCache.revision++
	allowCache.byClient = map[string]allowCacheEntry{}
	ipHashSalt.value = nil
	ipPrivacySettings.showRaw = false
	ipPrivacySettings.expiresAt = time.Time{}
	pending.Unlock()
	allowCache.Unlock()
	ipPrivacySettings.Unlock()
	ipHashSalt.Unlock()

	securityEvents.Lock()
	securityEvents.lastEmittedAt = map[string]time.Time{}
	securityEvents.Unlock()
}
