// Package resourcepressure is the single neutral owner for resource-pressure
// evaluation and pressure-aware admission policy.
package resourcepressure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	MaxSignals       = 96
	MaxReasonCodes   = 16
	DefaultFreshness = 2 * time.Minute
	SampleInterval   = 15 * time.Second
	RecoveryWindow   = 5 * time.Minute
	RatioHysteresis  = 0.05
)

type State string

const (
	StateUnknown     State = "UNKNOWN"
	StateNormal      State = "NORMAL"
	StateWarning     State = "WARNING"
	StateConstrained State = "CONSTRAINED"
	StateCritical    State = "CRITICAL"
	StateRecovering  State = "RECOVERING"
)

type ProviderStatus string

const (
	ProviderSupported   ProviderStatus = "SUPPORTED"
	ProviderUnavailable ProviderStatus = "UNAVAILABLE"
	ProviderUnsupported ProviderStatus = "UNSUPPORTED"
	ProviderStale       ProviderStatus = "STALE"
	ProviderError       ProviderStatus = "ERROR"
)

type Direction string

const (
	HigherIsWorse Direction = "HIGHER_IS_WORSE"
	LowerIsWorse  Direction = "LOWER_IS_WORSE"
)

type Signal struct {
	ID         string         `json:"id"`
	Status     ProviderStatus `json:"status"`
	Value      float64        `json:"value,omitempty"`
	Unit       string         `json:"unit,omitempty"`
	ObservedAt int64          `json:"observedAt,omitempty"`
	ExpiresAt  int64          `json:"expiresAt,omitempty"`
	ReasonCode string         `json:"reasonCode,omitempty"`
}

type Threshold struct {
	ID          string    `json:"id"`
	Direction   Direction `json:"direction"`
	Warning     float64   `json:"warning"`
	Constrained float64   `json:"constrained"`
	Critical    float64   `json:"critical"`
	Required    bool      `json:"required"`
}

type Snapshot struct {
	State             State    `json:"state"`
	PreviousState     State    `json:"previousState"`
	Signals           []Signal `json:"signals"`
	ReasonCodes       []string `json:"reasonCodes"`
	ObservationDigest string   `json:"observationDigest"`
	Revision          uint64   `json:"revision"`
	ObservedAt        int64    `json:"observedAt"`
	ChangedAt         int64    `json:"changedAt"`
}

type Evaluator struct {
	Thresholds []Threshold
	current    Snapshot
	pending    State
	pendingN   uint32
	pendingAt  int64
	recoverAt  int64
}

func DefaultThresholds() []Threshold {
	return []Threshold{
		{ID: "filesystem.data.free_ratio", Direction: LowerIsWorse, Warning: .20, Constrained: .10, Critical: .05, Required: true},
		{ID: "filesystem.data.free_bytes", Direction: LowerIsWorse, Warning: 2 << 30, Constrained: 1 << 30, Critical: 512 << 20, Required: true},
		{ID: "filesystem.data.free_inode_ratio", Direction: LowerIsWorse, Warning: .10, Constrained: .05, Critical: .02},
		{ID: "filesystem.temp.free_ratio", Direction: LowerIsWorse, Warning: .20, Constrained: .10, Critical: .05},
		{ID: "filesystem.temp.free_bytes", Direction: LowerIsWorse, Warning: 2 << 30, Constrained: 1 << 30, Critical: 512 << 20},
		{ID: "filesystem.temp.free_inode_ratio", Direction: LowerIsWorse, Warning: .10, Constrained: .05, Critical: .02},
		{ID: "sqlite.wal.bytes", Direction: HigherIsWorse, Warning: 256 << 20, Constrained: 512 << 20, Critical: 1 << 30},
		{ID: "sqlite.busy.rate", Direction: HigherIsWorse, Warning: .02, Constrained: .08, Critical: .20},
		{ID: "memory.used_ratio", Direction: HigherIsWorse, Warning: .80, Constrained: .90, Critical: .96},
		{ID: "cgroup.memory.used_ratio", Direction: HigherIsWorse, Warning: .80, Constrained: .90, Critical: .96},
		{ID: "psi.memory.some_avg10", Direction: HigherIsWorse, Warning: 10, Constrained: 30, Critical: 60},
		{ID: "process.fd.used_ratio", Direction: HigherIsWorse, Warning: .70, Constrained: .85, Critical: .95},
		{ID: "process.goroutines", Direction: HigherIsWorse, Warning: 1500, Constrained: 4000, Critical: 8000},
		{ID: "http.active", Direction: HigherIsWorse, Warning: 200, Constrained: 500, Critical: 1000},
		{ID: "audit.queue.used_ratio", Direction: HigherIsWorse, Warning: .60, Constrained: .80, Critical: .95},
		{ID: "operations.heavy.active", Direction: HigherIsWorse, Warning: 2, Constrained: 4, Critical: 8},
	}
}

func NewEvaluator(thresholds []Threshold) (*Evaluator, error) {
	if len(thresholds) == 0 || len(thresholds) > MaxSignals {
		return nil, errors.New("resource pressure thresholds are invalid")
	}
	seen := map[string]struct{}{}
	for _, threshold := range thresholds {
		if !safeID(threshold.ID) || (threshold.Direction != HigherIsWorse && threshold.Direction != LowerIsWorse) {
			return nil, errors.New("resource pressure threshold is invalid")
		}
		if _, exists := seen[threshold.ID]; exists {
			return nil, errors.New("resource pressure threshold is duplicated")
		}
		seen[threshold.ID] = struct{}{}
		if !validThresholdBounds(threshold) ||
			threshold.Direction == HigherIsWorse && !(threshold.Warning < threshold.Constrained && threshold.Constrained < threshold.Critical) ||
			threshold.Direction == LowerIsWorse && !(threshold.Warning > threshold.Constrained && threshold.Constrained > threshold.Critical) {
			return nil, errors.New("resource pressure threshold ordering is invalid")
		}
	}
	return &Evaluator{Thresholds: append([]Threshold(nil), thresholds...), current: Snapshot{State: StateUnknown}}, nil
}

func (e *Evaluator) Evaluate(now time.Time, signals []Signal) Snapshot {
	if e == nil {
		return Snapshot{State: StateUnknown, ReasonCodes: []string{"pressure_evaluator_unavailable"}, ObservedAt: now.Unix()}
	}
	copySignals := normalizeSignals(now, signals)
	previous := e.current.State
	if previous == "" {
		previous = StateUnknown
	}
	exitThresholds := previous == StateRecovering || severity(previous) >= severity(StateWarning)
	target, reasons := e.classify(now, copySignals, exitThresholds)
	next := e.transition(now, previous, target)
	changedAt := e.current.ChangedAt
	if next != previous || changedAt == 0 {
		changedAt = now.Unix()
	}
	revision := e.current.Revision
	if next != previous {
		revision++
	}
	if revision == 0 {
		revision = 1
	}
	snapshot := Snapshot{State: next, PreviousState: previous, Signals: copySignals, ReasonCodes: reasons,
		Revision: revision, ObservedAt: now.Unix(), ChangedAt: changedAt}
	snapshot.ObservationDigest = semanticDigest(struct {
		Signals []Signal
		Reasons []string
	}{copySignals, reasons})
	e.current = snapshot
	return snapshot
}

func (e *Evaluator) classify(now time.Time, signals []Signal, exitThresholds bool) (State, []string) {
	byID := make(map[string]Signal, len(signals))
	for _, signal := range signals {
		byID[signal.ID] = signal
	}
	worst := StateNormal
	reasons := make([]string, 0, 4)
	for _, threshold := range e.Thresholds {
		signal, exists := byID[threshold.ID]
		if !exists || signal.Status != ProviderSupported || signal.ExpiresAt <= now.Unix() {
			if threshold.Required {
				worst = maxState(worst, StateWarning)
				reasons = appendBounded(reasons, "required_signal_unavailable:"+threshold.ID)
			}
			continue
		}
		state := thresholdState(signal.Value, threshold, exitThresholds)
		if state != StateNormal {
			reasons = appendBounded(reasons, string(state)+":"+threshold.ID)
		}
		worst = maxState(worst, state)
	}
	if len(reasons) == 0 {
		reasons = []string{"within_configured_bounds"}
	}
	return worst, reasons
}

func (e *Evaluator) transition(now time.Time, current, target State) State {
	if target == StateCritical {
		e.resetPending()
		e.recoverAt = 0
		return StateCritical
	}
	if current == StateUnknown {
		return e.confirm(now, target, 2, target)
	}
	if current == StateRecovering {
		if target == StateNormal {
			if e.recoverAt == 0 {
				e.recoverAt = now.Unix()
			}
			if e.confirmed(now, StateNormal, 2) && now.Sub(time.Unix(e.recoverAt, 0)) >= RecoveryWindow {
				e.resetPending()
				e.recoverAt = 0
				return StateNormal
			}
			return StateRecovering
		}
		e.recoverAt = 0
		return e.confirm(now, target, 2, target)
	}
	if severity(target) > severity(current) {
		e.recoverAt = 0
		return e.confirm(now, target, 2, target)
	}
	if severity(target) < severity(current) {
		if target == StateNormal {
			next := e.confirm(now, target, 2, StateRecovering)
			if next == StateRecovering {
				e.recoverAt = now.Unix()
			}
			return next
		}
		e.recoverAt = 0
		return e.confirm(now, target, 2, target)
	}
	e.resetPending()
	return current
}

func (e *Evaluator) confirm(now time.Time, target State, count uint32, confirmed State) State {
	if e.confirmed(now, target, count) {
		e.resetPending()
		return confirmed
	}
	return e.current.State
}

func (e *Evaluator) confirmed(now time.Time, target State, count uint32) bool {
	if e.pending != target {
		e.pending, e.pendingN, e.pendingAt = target, 1, now.Unix()
		return count <= 1
	}
	elapsed := now.Sub(time.Unix(e.pendingAt, 0))
	if elapsed > DefaultFreshness {
		e.pendingN = 1
		e.pendingAt = now.Unix()
		return count <= 1
	}
	if elapsed >= SampleInterval {
		e.pendingN++
		e.pendingAt = now.Unix()
	}
	return e.pendingN >= count
}

func (e *Evaluator) resetPending() {
	e.pending, e.pendingN, e.pendingAt = "", 0, 0
}

func validThresholdBounds(threshold Threshold) bool {
	values := []float64{threshold.Warning, threshold.Constrained, threshold.Critical}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
	}
	if strings.HasSuffix(threshold.ID, ".ratio") {
		return threshold.Warning <= 1 && threshold.Constrained <= 1 && threshold.Critical <= 1
	}
	return threshold.Warning <= 1<<50 && threshold.Constrained <= 1<<50 && threshold.Critical <= 1<<50
}

type Admission struct {
	Allowed    bool   `json:"allowed"`
	ReasonCode string `json:"reasonCode"`
	RetryAfter int    `json:"retryAfterSeconds,omitempty"`
}

func Decide(state State, pressureClass string) Admission {
	if pressureClass == "essential" || pressureClass == "recovery_essential" {
		return Admission{Allowed: true, ReasonCode: "essential_operation_preserved"}
	}
	switch state {
	case StateCritical:
		if pressureClass == "interactive" || pressureClass == "status" || pressureClass == "security_critical" {
			return Admission{Allowed: true, ReasonCode: "critical_read_or_security_preserved"}
		}
		return Admission{ReasonCode: "resource_pressure_critical", RetryAfter: 30}
	case StateConstrained:
		if pressureClass == "expensive" || pressureClass == "optional" || pressureClass == "heavy_mutation" || pressureClass == "bounded_component" {
			return Admission{ReasonCode: "resource_pressure_constrained", RetryAfter: 10}
		}
	case StateWarning:
		if pressureClass == "optional" {
			return Admission{ReasonCode: "resource_pressure_warning", RetryAfter: 5}
		}
	case StateRecovering:
		if pressureClass == "expensive" || pressureClass == "optional" || pressureClass == "heavy_mutation" || pressureClass == "bounded_component" {
			return Admission{ReasonCode: "resource_pressure_recovering", RetryAfter: 15}
		}
	case StateUnknown:
		if pressureClass == "heavy_mutation" {
			return Admission{ReasonCode: "resource_pressure_unknown", RetryAfter: 10}
		}
	}
	return Admission{Allowed: true, ReasonCode: "pressure_admission_allowed"}
}

func normalizeSignals(now time.Time, signals []Signal) []Signal {
	if len(signals) > MaxSignals {
		signals = signals[:MaxSignals]
	}
	result := make([]Signal, 0, len(signals))
	seen := map[string]struct{}{}
	for _, signal := range signals {
		if !safeID(signal.ID) || !validProviderStatus(signal.Status) {
			continue
		}
		if _, exists := seen[signal.ID]; exists {
			continue
		}
		seen[signal.ID] = struct{}{}
		if signal.ObservedAt == 0 {
			signal.ObservedAt = now.Unix()
		}
		if signal.ExpiresAt == 0 {
			signal.ExpiresAt = now.Add(DefaultFreshness).Unix()
		}
		if signal.Status == ProviderSupported && invalidSignalValue(signal) {
			signal.Status, signal.Value, signal.ReasonCode = ProviderError, 0, "provider_numeric_value_invalid"
		}
		if signal.ExpiresAt <= now.Unix() && signal.Status == ProviderSupported {
			signal.Status = ProviderStale
			signal.ReasonCode = "observation_expired"
		}
		if len(signal.ReasonCode) > 96 {
			signal.ReasonCode = "provider_reason_too_large"
		}
		result = append(result, signal)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func invalidSignalValue(signal Signal) bool {
	if math.IsNaN(signal.Value) || math.IsInf(signal.Value, 0) || signal.Value < 0 {
		return true
	}
	return signal.Unit == "ratio" && signal.Value > 1
}

func thresholdState(value float64, threshold Threshold, exitThresholds bool) State {
	warning, constrained, critical := threshold.Warning, threshold.Constrained, threshold.Critical
	if exitThresholds && strings.HasSuffix(threshold.ID, ".ratio") {
		if threshold.Direction == HigherIsWorse {
			warning = math.Max(0, warning-RatioHysteresis)
			constrained = math.Max(0, constrained-RatioHysteresis)
			critical = math.Max(0, critical-RatioHysteresis)
		} else {
			warning = math.Min(1, warning+RatioHysteresis)
			constrained = math.Min(1, constrained+RatioHysteresis)
			critical = math.Min(1, critical+RatioHysteresis)
		}
	}
	if threshold.Direction == HigherIsWorse {
		switch {
		case value >= critical:
			return StateCritical
		case value >= constrained:
			return StateConstrained
		case value >= warning:
			return StateWarning
		}
	} else {
		switch {
		case value <= critical:
			return StateCritical
		case value <= constrained:
			return StateConstrained
		case value <= warning:
			return StateWarning
		}
	}
	return StateNormal
}

func severity(state State) int {
	switch state {
	case StateCritical:
		return 4
	case StateConstrained:
		return 3
	case StateWarning:
		return 2
	case StateRecovering:
		return 1
	case StateNormal:
		return 0
	default:
		return -1
	}
}

func maxState(left, right State) State {
	if severity(right) > severity(left) {
		return right
	}
	return left
}

func appendBounded(values []string, value string) []string {
	if len(values) >= MaxReasonCodes {
		return values
	}
	return append(values, value)
}

func safeID(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

func validProviderStatus(status ProviderStatus) bool {
	return status == ProviderSupported || status == ProviderUnavailable || status == ProviderUnsupported || status == ProviderStale || status == ProviderError
}

func semanticDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
