package ipmonitor

import (
	"sync"
	"time"

	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
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
}{byClient: map[string]map[string]pendingIP{}}

var allowCache = struct {
	sync.Mutex
	byClient map[string]allowCacheEntry
}{byClient: map[string]allowCacheEntry{}}

var allowCacheRefresh = struct {
	sync.Mutex
	inFlight map[string]struct{}
}{inFlight: map[string]struct{}{}}

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
	pending.Lock()
	pending.byClient = map[string]map[string]pendingIP{}
	pending.Unlock()

	allowCache.Lock()
	allowCache.byClient = map[string]allowCacheEntry{}
	allowCache.Unlock()

	allowCacheRefresh.Lock()
	allowCacheRefresh.inFlight = map[string]struct{}{}
	allowCacheRefresh.Unlock()

	securityEvents.Lock()
	securityEvents.lastEmittedAt = map[string]time.Time{}
	securityEvents.Unlock()

	ipHashSalt.Lock()
	ipHashSalt.value = nil
	ipHashSalt.Unlock()

	ipPrivacySettings.Lock()
	ipPrivacySettings.showRaw = false
	ipPrivacySettings.expiresAt = time.Time{}
	ipPrivacySettings.Unlock()
}
