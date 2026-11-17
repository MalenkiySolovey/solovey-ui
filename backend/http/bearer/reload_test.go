package bearer

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPIV2OverlappingReloadCannotRepublishOldAuthorization(t *testing.T) {
	for _, latestFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(latestFails), func(t *testing.T) {
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			h := &Handler{tokens: map[string]TokenInMemory{"earlier": {ID: 1}}}
			h.LoadTokens = func() ([]byte, error) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
					return json.Marshal([]TokenInMemory{{ID: 1, TokenHash: "earlier"}})
				}
				if latestFails {
					return nil, errors.New("fixture load failed")
				}
				return json.Marshal([]TokenInMemory{{ID: 2, TokenHash: "current"}})
			}
			go func() { h.ReloadTokens(); close(done) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("reload did not reach barrier")
			}
			h.tokensMu.RLock()
			during := len(h.tokens)
			h.tokensMu.RUnlock()
			h.ReloadTokens()
			close(release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("older reload did not finish")
			}
			if during != 0 {
				t.Fatal("old snapshot remained authorized during reload")
			}
			h.tokensMu.RLock()
			defer h.tokensMu.RUnlock()
			if _, stale := h.tokens["earlier"]; stale {
				t.Fatal("older loader republished an invalidated snapshot")
			}
			if _, current := h.tokens["current"]; current == latestFails {
				t.Fatal("snapshot does not reflect the latest reload outcome")
			}
		})
	}
}
