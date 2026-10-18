package operationcoordination

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func lane(events []JournalEvent) JournalReader {
	return func(_ context.Context, after uint64, limit int) ([]JournalEvent, error) {
		result := []JournalEvent{}
		for _, event := range events {
			if event.Sequence > after && len(result) < limit {
				result = append(result, event)
			}
		}
		return result, nil
	}
}

func TestJournalReconnectPreservesOwnerOrderAcrossClockRollback(t *testing.T) {
	u := []JournalEvent{
		ProjectEvent(NewReference("update", "panel", "update-operation:one"), 1, 1, "admitted", "DOWNLOADING", "", 10),
		ProjectEvent(NewReference("update", "panel", "update-operation:one"), 2, 2, "staged", "DOWNLOADED", "", 1),
	}
	d := []JournalEvent{ProjectEvent(NewReference("data", "database", "data-operation:one"), 1, 3, "restore_applied", "APPLIED", "", 5)}
	cursor := JournalCursor{}
	seen := map[string]bool{}
	for range 3 {
		page, err := ReadJournal(t.Context(), lane(u), lane(d), cursor, 1)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		key := page.Items[0].JournalRef
		if seen[key] {
			t.Fatalf("cursor replayed %s", key)
		}
		seen[key] = true
		cursor = page.Next
	}
	if cursor.Update != 2 || cursor.Data != 1 || len(seen) != 3 {
		t.Fatalf("cursor lost event: %+v", cursor)
	}
	final, err := ReadJournal(t.Context(), lane(u), lane(d), cursor, 1)
	if err != nil || len(final.Items) != 0 || final.HasMore || final.Next != cursor {
		t.Fatalf("reconnect=%+v %v", final, err)
	}
}

func TestJournalIsBoundedCodeOnlyAndDoesNotAdvanceOnFailure(t *testing.T) {
	event := ProjectEvent(NewReference("data", "database", "data-operation:one"), 1, 1,
		"Bearer deterministic fake secret", "Bearer deterministic fake secret", "Bearer deterministic fake secret", 10)
	payload, _ := json.Marshal(event)
	if strings.Contains(string(payload), "fake secret") {
		t.Fatalf("journal copied raw metadata: %s", payload)
	}
	malformed := ProjectEvent(Reference{Owner: "Bearer deterministic fake secret"}, 1, 1, "test", "TEST", "", 10)
	malformedPayload, _ := json.Marshal(malformed)
	if strings.Contains(string(malformedPayload), "fake secret") || malformed.JournalRef != "unavailable-journal:1" {
		t.Fatalf("journal copied malformed reference: %s", malformedPayload)
	}
	empty := lane(nil)
	if _, err := ReadJournal(t.Context(), empty, empty, JournalCursor{}, MaxJournalPage+1); err == nil {
		t.Fatal("unbounded page accepted")
	}
	cause := errors.New("owner unavailable")
	failed := func(context.Context, uint64, int) ([]JournalEvent, error) { return nil, cause }
	start := JournalCursor{Update: 3, Data: 4}
	page, err := ReadJournal(t.Context(), empty, failed, start, 1)
	if !errors.Is(err, cause) || page.Next != start || len(page.Items) != 0 {
		t.Fatalf("failed read advanced: %+v %v", page, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadJournal(ctx, empty, empty, start, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if (Reference{}).Correlation().JobID == "" {
		t.Fatal("zero reference lacks safe projection")
	}
}
