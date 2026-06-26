package realtimehttp

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"sort"
	"sync"
	"time"

	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

const (
	wsTokenSweepInterval = time.Minute
	MaxTokens            = 4096
)

type realtimeToken struct {
	user      string
	expiresAt time.Time
}

var wsTokens = struct {
	sync.Mutex
	tokens          map[[sha256.Size]byte]realtimeToken
	lastSweep       time.Time
	sweepTimer      *time.Timer
	sweepGeneration uint64
}{
	tokens: map[[sha256.Size]byte]realtimeToken{},
}

func init() {
	dbhooks.RegisterResetHook("api.ws_tokens", func() {
		_ = sweepAllWSTokens()
	})
	service.RegisterWSTokenInvalidationHook("api.ws_tokens", sweepAllWSTokens)
}

func wsTokenDigest(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

func StoreToken(token string, user string, expiresAt time.Time) {
	now := time.Now()
	wsTokens.Lock()
	maybeSweepWSTokensLocked(now)
	wsTokens.tokens[wsTokenDigest(token)] = realtimeToken{user: user, expiresAt: expiresAt}
	enforceWSTokenCapLocked()
	scheduleWSTokenSweepLocked()
	wsTokens.Unlock()
}

func ConsumeToken(token string) (string, bool) { return consumeWSToken(token) }

func ResetTokens() int { return sweepAllWSTokens() }

func TokenCount() int {
	wsTokens.Lock()
	defer wsTokens.Unlock()
	return len(wsTokens.tokens)
}

func HasToken(token string) bool {
	wsTokens.Lock()
	defer wsTokens.Unlock()
	_, ok := wsTokens.tokens[wsTokenDigest(token)]
	return ok
}

func SweepExpired(now time.Time) {
	wsTokens.Lock()
	defer wsTokens.Unlock()
	sweepWSTokensLocked(now)
}

func consumeWSToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	wsTokens.Lock()
	defer wsTokens.Unlock()

	candidate := wsTokenDigest(token)
	keys := make([][sha256.Size]byte, 0, len(wsTokens.tokens))
	for key := range wsTokens.tokens {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i][:], keys[j][:]) < 0
	})

	matched := 0
	var matchedKey [sha256.Size]byte
	matchedExpiresAtUnixNano := int64(0)
	matchedUserIndex := 0
	users := make([]string, len(keys))
	for i, key := range keys {
		data := wsTokens.tokens[key]
		users[i] = data.user
		eq := subtle.ConstantTimeCompare(candidate[:], key[:])
		subtle.ConstantTimeCopy(eq, matchedKey[:], key[:])
		matched = subtle.ConstantTimeSelect(eq, 1, matched)
		matchedExpiresAtUnixNano = constantTimeSelectInt64(eq, data.expiresAt.UnixNano(), matchedExpiresAtUnixNano)
		matchedUserIndex = subtle.ConstantTimeSelect(eq, i+1, matchedUserIndex)
	}
	delete(wsTokens.tokens, matchedKey)
	now := time.Now()
	matchedExpiresAt := time.Unix(0, matchedExpiresAtUnixNano)
	if matched != 1 || now.After(matchedExpiresAt) {
		return "", false
	}
	return users[matchedUserIndex-1], true
}

func constantTimeSelectInt64(v int, x int64, y int64) int64 {
	mask := int64(-v)
	return (x & mask) | (y &^ mask)
}

func maybeSweepWSTokensLocked(now time.Time) {
	if wsTokens.lastSweep.IsZero() || now.Sub(wsTokens.lastSweep) > wsTokenSweepInterval {
		sweepWSTokensLocked(now)
	}
}

func runWSTokenSweep(generation uint64) {
	wsTokens.Lock()
	defer wsTokens.Unlock()
	if generation != wsTokens.sweepGeneration {
		return
	}
	wsTokens.sweepTimer = nil
	sweepWSTokensLocked(time.Now())
	scheduleWSTokenSweepLocked()
}

func scheduleWSTokenSweepLocked() {
	if len(wsTokens.tokens) == 0 || wsTokens.sweepTimer != nil {
		return
	}
	generation := wsTokens.sweepGeneration
	wsTokens.sweepTimer = time.AfterFunc(wsTokenSweepInterval, func() {
		runWSTokenSweep(generation)
	})
}

func sweepWSTokensLocked(now time.Time) {
	for token, data := range wsTokens.tokens {
		if now.After(data.expiresAt) {
			delete(wsTokens.tokens, token)
		}
	}
	wsTokens.lastSweep = now
	enforceWSTokenCapLocked()
}

func sweepAllWSTokens() int {
	wsTokens.Lock()
	defer wsTokens.Unlock()
	return sweepAllWSTokensLocked()
}

func sweepAllWSTokensLocked() int {
	count := len(wsTokens.tokens)
	wsTokens.tokens = map[[sha256.Size]byte]realtimeToken{}
	wsTokens.lastSweep = time.Time{}
	if wsTokens.sweepTimer != nil {
		wsTokens.sweepTimer.Stop()
		wsTokens.sweepTimer = nil
	}
	wsTokens.sweepGeneration++
	return count
}

func enforceWSTokenCapLocked() {
	overflow := len(wsTokens.tokens) - MaxTokens
	if overflow <= 0 {
		return
	}
	entries := make([]struct {
		token     [sha256.Size]byte
		expiresAt time.Time
	}, 0, len(wsTokens.tokens))
	for token, data := range wsTokens.tokens {
		entries = append(entries, struct {
			token     [sha256.Size]byte
			expiresAt time.Time
		}{token: token, expiresAt: data.expiresAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].expiresAt.Equal(entries[j].expiresAt) {
			return bytes.Compare(entries[i].token[:], entries[j].token[:]) < 0
		}
		return entries[i].expiresAt.Before(entries[j].expiresAt)
	})
	for i := 0; i < overflow; i++ {
		delete(wsTokens.tokens, entries[i].token)
	}
}
