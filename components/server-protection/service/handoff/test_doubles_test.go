package handoff

import (
	"context"
	"sync"
)

type FakeProcessExecutor struct {
	mu    sync.Mutex
	Calls []Fence
	Err   error
}

func (f *FakeProcessExecutor) RestartSingBox(_ context.Context, fence Fence) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, fence)
	return f.Err
}
