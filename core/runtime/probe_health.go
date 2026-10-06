package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

type ProbeSource string

const (
	ProbeSourceManual     ProbeSource = "manual"
	ProbeSourceFailover   ProbeSource = "failover"
	ProbeSourceDiagnostic ProbeSource = "diagnostic"
	probeHealthMaxAge                 = 10 * time.Minute
	probeHealthMaxEntries             = 1024
)

// OutboundHealth is a recent observation of one runtime outbound. TargetID is a
// digest: probe URLs may contain credentials and never enter the projection.
type OutboundHealth struct {
	Tag       string      `json:"tag"`
	Source    ProbeSource `json:"source"`
	TargetID  string      `json:"targetId"`
	Status    string      `json:"status"`
	DelayMs   uint16      `json:"delayMs,omitempty"`
	Error     string      `json:"error,omitempty"`
	CheckedAt int64       `json:"checkedAt"`
}

type probeObservation struct {
	sequence uint64
	touched  time.Time
	checked  time.Time
	health   OutboundHealth
}

type probeTicket struct {
	entry    *probeObservation
	sequence uint64
	tag      string
	source   ProbeSource
	targetID string
}

// recentProbeHealth is scoped to Core, including pending observations. There
// are no durable rows, global caches, or unbounded per-tag tombstones.
type recentProbeHealth struct {
	mu       sync.RWMutex
	sequence uint64
	entries  map[string]*probeObservation
}

func (h *recentProbeHealth) begin(tag, target string, source ProbeSource, now time.Time) probeTicket {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = make(map[string]*probeObservation)
	}
	for key, entry := range h.entries {
		if now.Sub(entry.touched) >= probeHealthMaxAge {
			delete(h.entries, key)
		}
	}
	h.sequence++
	entry := h.entries[tag]
	if entry == nil {
		entry = &probeObservation{}
		h.entries[tag] = entry
	}
	entry.sequence = h.sequence
	entry.touched = now
	if len(h.entries) > probeHealthMaxEntries {
		keys := make([]string, 0, len(h.entries))
		for key := range h.entries {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := h.entries[keys[i]], h.entries[keys[j]]
			if a.touched.Equal(b.touched) {
				return a.sequence < b.sequence
			}
			return a.touched.Before(b.touched)
		})
		for _, key := range keys[:len(keys)-probeHealthMaxEntries] {
			delete(h.entries, key)
		}
	}
	digest := sha256.Sum256([]byte(target))
	return probeTicket{entry: entry, sequence: entry.sequence, tag: tag, source: source, targetID: hex.EncodeToString(digest[:])}
}

func (h *recentProbeHealth) complete(ticket probeTicket, result CheckOutboundResult, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := h.entries[ticket.tag]
	if entry == nil || entry != ticket.entry || entry.sequence != ticket.sequence || now.Sub(entry.touched) >= probeHealthMaxAge {
		return
	}
	status := "down"
	if result.OK {
		status = "healthy"
	} else if result.Error == CheckOutboundErrorCanceled {
		status = "unknown"
	}
	entry.checked, entry.touched = now, now
	entry.health = OutboundHealth{Tag: ticket.tag, Source: ticket.source, TargetID: ticket.targetID,
		Status: status, DelayMs: result.Delay, Error: safeProbeError(result.Error), CheckedAt: now.Unix()}
}

func safeProbeError(value string) string {
	switch value {
	case "", CheckOutboundErrorInvalidRequest, CheckOutboundErrorCoreUnavailable, CheckOutboundErrorNotFound,
		CheckOutboundErrorTimeout, CheckOutboundErrorCanceled, CheckOutboundErrorNetwork, CheckOutboundErrorFailed:
		return value
	default:
		return CheckOutboundErrorFailed
	}
}

func (h *recentProbeHealth) snapshot(now time.Time) map[string]OutboundHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]OutboundHealth)
	for tag, entry := range h.entries {
		if entry.checked.IsZero() || now.Before(entry.checked) || now.Sub(entry.checked) >= probeHealthMaxAge {
			continue
		}
		result[tag] = entry.health
	}
	return result
}

func (h *recentProbeHealth) remove(tag string) {
	h.mu.Lock()
	delete(h.entries, tag)
	h.mu.Unlock()
}

func (h *recentProbeHealth) reset() {
	h.mu.Lock()
	h.entries = nil
	h.mu.Unlock()
}

// OutboundHealthSnapshot consumes runtime probe state without executing probes
// or pruning it. It must also be callable inside a leased stats operation: no
// recursive lifecycle read lock, which would deadlock behind a pending Stop.
// Target mutation invalidates observations before touching the manager.
func (c *Core) OutboundHealthSnapshot() map[string]OutboundHealth {
	result := make(map[string]OutboundHealth)
	if c == nil {
		return result
	}
	c.access.RLock()
	defer c.access.RUnlock()
	if !c.isRunning || c.instance == nil {
		return result
	}
	return c.probeHealth.snapshot(time.Now())
}
