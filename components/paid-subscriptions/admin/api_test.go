//go:build !minimal

package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTelegramActionsStaleCleanupPreservesNewRegistration(t *testing.T) {
	cleanupOld := RegisterTelegramActions(
		func(context.Context, string) (int, int, error) { return 1, 0, nil },
		func(context.Context, uint, bool) (string, error) { return "old", nil },
	)
	cleanupOld()
	cleanupNew := RegisterTelegramActions(
		func(context.Context, string) (int, int, error) { return 2, 0, nil },
		func(context.Context, uint, bool) (string, error) { return "new", nil },
	)
	t.Cleanup(cleanupNew)

	cleanupOld()
	telegramActions.RLock()
	broadcast := telegramActions.broadcast
	telegramActions.RUnlock()
	if broadcast == nil {
		t.Fatal("stale cleanup removed the new Telegram actions")
	}
	sent, _, err := broadcast(context.Background(), "test")
	if err != nil || sent != 2 {
		t.Fatalf("new Telegram action changed: sent=%d err=%v", sent, err)
	}
}

// The frontend's isMsg() requires the keys success, msg AND obj to all be
// present; omitempty on any of them makes the client report "unknown data".
func TestApiMsgAlwaysIncludesAllKeys(t *testing.T) {
	for _, m := range []apiMsg{
		{Success: true},               // respOK(c, nil)
		{Success: true, Obj: []int{}}, // respOK(c, list)
		{Success: false, Msg: "x"},    // respFail
	} {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, key := range []string{`"success"`, `"msg"`, `"obj"`} {
			if !strings.Contains(s, key) {
				t.Errorf("apiMsg JSON %s missing required key %s", s, key)
			}
		}
	}
}
