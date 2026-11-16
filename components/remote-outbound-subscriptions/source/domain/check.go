//go:build !minimal

package remote

import (
	"context"
	"sync"
	"time"
)

const checkConcurrency = 8

type CheckOutboundResult struct {
	OK    bool
	Delay uint16
	Error string
}

type CheckResult struct {
	ConnectionId uint                `json:"connectionId"`
	OutboundTag  string              `json:"outboundTag"`
	Skipped      bool                `json:"skipped,omitempty"`
	Error        string              `json:"error,omitempty"`
	Result       CheckOutboundResult `json:"result"`
}

func CheckConnection(ctx context.Context, db Store, id uint, target string) (*CheckResult, error) {
	var connection RemoteOutboundConnection
	if err := db.LoadConnection(id, &connection); err != nil {
		return nil, err
	}
	return CheckConnectionRecordWithDB(ctx, db, connection, target), nil
}
func CheckSubscription(ctx context.Context, db Store, subscriptionID uint, target string) ([]CheckResult, error) {
	var connections []RemoteOutboundConnection
	if err := db.ListOrderedSubscriptionConnections(subscriptionID, &connections); err != nil {
		return nil, err
	}
	return CheckConnectionRecordsWithDB(ctx, db, connections, target), nil
}
func CheckAll(ctx context.Context, db Store, target string) ([]CheckResult, error) {
	var connections []RemoteOutboundConnection
	if err := db.ListEnabledConnections(&connections); err != nil {
		return nil, err
	}
	return CheckConnectionRecordsWithDB(ctx, db, connections, target), nil
}

func CheckConnectionRecordsWithDB(ctx context.Context, db Store, connections []RemoteOutboundConnection, target string) []CheckResult {
	if ctx == nil {
		ctx = context.Background()
	}
	results := make([]CheckResult, len(connections))
	if len(connections) == 0 {
		return results
	}
	workerCount := min(checkConcurrency, len(connections))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				item := connections[index]
				if err := ctx.Err(); err != nil {
					results[index] = cancelledCheckResult(db, item, err)
					continue
				}
				checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				results[index] = *CheckConnectionRecordWithDB(checkCtx, db, item, target)
				cancel()
			}
		}()
	}
	for index := range connections {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func cancelledCheckResult(db Store, connection RemoteOutboundConnection, err error) CheckResult {
	return CheckResult{
		ConnectionId: connection.Id,
		OutboundTag:  connection.OutboundTag,
		Error:        db.ClassifyCheckError(err),
	}
}
func CheckConnectionRecordWithDB(ctx context.Context, db Store, connection RemoteOutboundConnection, target string) *CheckResult {
	result := &CheckResult{
		ConnectionId: connection.Id,
		OutboundTag:  connection.OutboundTag,
	}
	switch {
	case !connection.Enabled:
		result.Skipped = true
		result.Error = "connection is disabled"
	default:
		result.Result = db.ProbeConnection(ctx, connection, target)
		result.Error = result.Result.Error
	}
	return result
}
