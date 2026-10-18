package operationcoordination

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

const MaxJournalPage = 200

type Correlation struct {
	Reference
	JobID string `json:"jobId"`
}

func (r Reference) Correlation() Correlation {
	id := strings.TrimPrefix(r.CorrelationID, "operation:")
	if !strings.HasPrefix(r.CorrelationID, "operation:") || len(id) != 48 || strings.Trim(id, "0123456789abcdef") != "" ||
		r.ChangeID != "change:"+id || !referenceToken(r.Owner, 64) || !referenceToken(r.ResourceID, 256) ||
		!referenceToken(r.OperationID, 96) || !r.HostRef.IsLocal() {
		r = NewReference(r.Owner, r.ResourceID, r.OperationID)
		id = strings.TrimPrefix(r.CorrelationID, "operation:")
	}
	return Correlation{Reference: r, JobID: "job:" + id}
}

// JournalEvent is a bounded index projection, never a copied owner payload or
// recovery instruction. Owner journals retain their existing distinct storage.
type JournalEvent struct {
	Correlation
	Sequence   uint64 `json:"sequence"`
	Revision   uint64 `json:"revision"`
	Type       string `json:"type"`
	Phase      string `json:"phase"`
	Severity   string `json:"severity"`
	ReasonCode string `json:"reasonCode,omitempty"`
	JournalRef string `json:"journalRef"`
	CreatedAt  int64  `json:"createdAt"`
}

func ProjectEvent(reference Reference, sequence, revision uint64, event, phase, reason string, createdAt int64) JournalEvent {
	if !referenceToken(event, 96) {
		event = "owner_event_invalid"
	}
	if !referenceToken(phase, 64) {
		phase = "UNKNOWN"
	}
	problem := (Change{Reference: reference, Phase: phase}).WithProblem(reason).Problem
	severity := "info"
	if problem != nil {
		reason, severity = problem.Code, "warn"
	}
	correlation := reference.Correlation()
	return JournalEvent{Correlation: correlation, Sequence: sequence, Revision: revision,
		Type: event, Phase: phase, Severity: severity, ReasonCode: reason,
		JournalRef: correlation.Owner + "-journal:" + strconv.FormatUint(sequence, 10), CreatedAt: createdAt}
}

type JournalCursor struct {
	Update uint64 `json:"update"`
	Data   uint64 `json:"data"`
}
type JournalPage struct {
	Items   []JournalEvent `json:"items"`
	Next    JournalCursor  `json:"next"`
	HasMore bool           `json:"hasMore"`
	Limit   int            `json:"limit"`
}
type JournalReader func(context.Context, uint64, int) ([]JournalEvent, error)

// ReadJournal merges two bounded current owner lanes while preserving each
// lane's sequence even when wall-clock time moves backwards. Cursors advance
// only for emitted rows. Realtime reconnect can fetch this index then reread
// the owner operation; it never reconstructs recovery from event order.
func ReadJournal(ctx context.Context, update, data JournalReader, after JournalCursor, limit int) (JournalPage, error) {
	result := JournalPage{Items: []JournalEvent{}, Next: after, Limit: limit}
	if ctx == nil || update == nil || data == nil || limit < 1 || limit > MaxJournalPage {
		return result, errors.New("journal request invalid")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	u, err := update(ctx, after.Update, limit+1)
	if err != nil {
		return result, err
	}
	d, err := data(ctx, after.Data, limit+1)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !validLane(u, "update", after.Update, limit+1) || !validLane(d, "data", after.Data, limit+1) {
		return result, errors.New("journal owner lane invalid")
	}
	ui, di := 0, 0
	for len(result.Items) < limit && (ui < len(u) || di < len(d)) {
		if err := ctx.Err(); err != nil {
			return JournalPage{}, err
		}
		if di >= len(d) || ui < len(u) && u[ui].CreatedAt < d[di].CreatedAt {
			result.Items = append(result.Items, u[ui])
			result.Next.Update = u[ui].Sequence
			ui++
		} else {
			result.Items = append(result.Items, d[di])
			result.Next.Data = d[di].Sequence
			di++
		}
	}
	result.HasMore = ui < len(u) || di < len(d)
	return result, nil
}

func validLane(events []JournalEvent, owner string, after uint64, limit int) bool {
	if len(events) > limit {
		return false
	}
	for _, event := range events {
		if event.Owner != owner || event.Sequence <= after {
			return false
		}
		after = event.Sequence
	}
	return true
}
