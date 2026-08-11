// Package health provides bounded, side-effect-free resource health checks.
package health

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusOK                Status = "ok"
	StatusDegraded          Status = "degraded"
	StatusMissingCapability Status = "missing_capability"
)

const DefaultTimeout = 5 * time.Second
const maxHealthCheckers = 256

// Result intentionally contains only stable codes: health output is safe for
// diagnostics and recovery artifacts and cannot disclose listener paths.
type Result struct {
	ResourceID string `json:"resourceId"`
	Status     Status `json:"status"`
	Check      string `json:"check"`
	FactCode   string `json:"factCode"`
}

type Checker interface {
	ResourceID() string
	Check(context.Context) Result
}

// Matcher is optional for dynamic resources whose stable IDs are known only at
// check time (for example a component-owned published site).
type Matcher interface{ Matches(string) bool }

type registeredChecker struct {
	token   uint64
	checker Checker
}

type Registry struct {
	mu        sync.RWMutex
	nextToken uint64
	checkers  map[string]registeredChecker
}

func NewRegistry() *Registry { return &Registry{checkers: make(map[string]registeredChecker)} }

func (r *Registry) Register(checker Checker) (func(), error) {
	if checker == nil || strings.TrimSpace(checker.ResourceID()) == "" {
		return nil, errors.New("health checker and resource id are required")
	}
	id := strings.TrimSpace(checker.ResourceID())
	r.mu.Lock()
	if _, exists := r.checkers[id]; exists {
		r.mu.Unlock()
		return nil, errors.New("health checker resource is already registered: " + id)
	}
	if len(r.checkers) >= maxHealthCheckers {
		r.mu.Unlock()
		return nil, errors.New("health checker capacity exceeded")
	}
	token := r.nextToken
	r.nextToken++
	r.checkers[id] = registeredChecker{token: token, checker: checker}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if current, exists := r.checkers[id]; exists && current.token == token {
				delete(r.checkers, id)
			}
			r.mu.Unlock()
		})
	}, nil
}

func (r *Registry) Check(ctx context.Context, resourceID string) Result {
	resourceID = strings.TrimSpace(resourceID)
	r.mu.RLock()
	entry, exact := r.checkers[resourceID]
	checker := entry.checker
	ambiguous := false
	if !exact {
		for _, registered := range r.checkers {
			candidate := registered.checker
			if matcher, ok := candidate.(Matcher); ok && matcher.Matches(resourceID) {
				if checker != nil {
					ambiguous = true
					break
				}
				checker = candidate
			}
		}
	}
	r.mu.RUnlock()
	if ambiguous {
		return Result{ResourceID: resourceID, Status: StatusMissingCapability, Check: "internal_health", FactCode: "health_check_ambiguous"}
	}
	if checker == nil {
		return Result{ResourceID: resourceID, Status: StatusMissingCapability, Check: "internal_health", FactCode: "health_check_unavailable"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	resultCh := make(chan Result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				resultCh <- Result{ResourceID: resourceID, Status: StatusDegraded, Check: "internal_health", FactCode: "health_check_failed"}
			}
		}()
		resultCh <- checker.Check(ctx)
	}()
	select {
	case result := <-resultCh:
		return normalize(resourceID, result)
	case <-ctx.Done():
		return Result{ResourceID: resourceID, Status: StatusDegraded, Check: "internal_health", FactCode: "health_check_timeout"}
	}
}

func (r *Registry) CheckAll(ctx context.Context, resourceIDs []string) []Result {
	ids := append([]string(nil), resourceIDs...)
	sort.Strings(ids)
	result := make([]Result, 0, len(ids))
	for _, id := range ids {
		result = append(result, r.Check(ctx, id))
	}
	return result
}

func normalize(resourceID string, result Result) Result {
	result.ResourceID = resourceID
	if result.Status != StatusOK && result.Status != StatusDegraded && result.Status != StatusMissingCapability {
		result.Status = StatusDegraded
	}
	if result.Check == "" {
		result.Check = "internal_health"
	}
	if result.FactCode == "" {
		result.FactCode = "health_check_failed"
	}
	return result
}

var Default = NewRegistry()

func Register(checker Checker) (func(), error)            { return Default.Register(checker) }
func Check(ctx context.Context, resourceID string) Result { return Default.Check(ctx, resourceID) }
