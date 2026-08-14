//go:build !minimal

package remote

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"gorm.io/gorm"
)

const checkConcurrency = 8

type CheckResult struct {
	ConnectionId uint                            `json:"connectionId"`
	OutboundTag  string                          `json:"outboundTag"`
	Skipped      bool                            `json:"skipped,omitempty"`
	Error        string                          `json:"error,omitempty"`
	Result       coreruntime.CheckOutboundResult `json:"result"`
}
type tempCoreCheckConfig struct {
	Outbounds []json.RawMessage
	CheckTags []string
}

func CheckConnection(ctx context.Context, db *gorm.DB, id uint, target string) (*CheckResult, error) {
	var connection RemoteOutboundConnection
	if err := db.First(&connection, id).Error; err != nil {
		return nil, err
	}
	return CheckConnectionRecordWithDB(ctx, db, connection, target), nil
}
func CheckSubscription(ctx context.Context, db *gorm.DB, subscriptionID uint, target string) ([]CheckResult, error) {
	var connections []RemoteOutboundConnection
	if err := db.
		Where("subscription_id = ?", subscriptionID).
		Order(entityorder.Clause).
		Find(&connections).Error; err != nil {
		return nil, err
	}
	return CheckConnectionRecordsWithDB(ctx, db, connections, target), nil
}
func CheckAll(ctx context.Context, db *gorm.DB, target string) ([]CheckResult, error) {
	var connections []RemoteOutboundConnection
	if err := db.
		Where("enabled = ?", true).
		Order("subscription_id ASC, sort_order ASC, id ASC").
		Find(&connections).Error; err != nil {
		return nil, err
	}
	return CheckConnectionRecordsWithDB(ctx, db, connections, target), nil
}
func CheckConnectionRecords(ctx context.Context, connections []RemoteOutboundConnection, target string) []CheckResult {
	return CheckConnectionRecordsWithDB(ctx, nil, connections, target)
}
func CheckConnectionRecordsWithDB(ctx context.Context, db *gorm.DB, connections []RemoteOutboundConnection, target string) []CheckResult {
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
					results[index] = cancelledCheckResult(item, err)
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

func cancelledCheckResult(connection RemoteOutboundConnection, err error) CheckResult {
	return CheckResult{
		ConnectionId: connection.Id,
		OutboundTag:  connection.OutboundTag,
		Error:        coreruntime.ClassifyOutboundCheckError(err),
	}
}
func CheckConnectionRecordWithDB(ctx context.Context, db *gorm.DB, connection RemoteOutboundConnection, target string) *CheckResult {
	result := &CheckResult{
		ConnectionId: connection.Id,
		OutboundTag:  connection.OutboundTag,
	}
	switch {
	case !connection.Enabled:
		result.Skipped = true
		result.Error = "connection is disabled"
	default:
		result.Result = CheckConnectionWithTempCoreDB(ctx, db, connection, target)
		result.Error = result.Result.Error
	}
	return result
}
func CheckConnectionWithTempCoreDB(ctx context.Context, db *gorm.DB, connection RemoteOutboundConnection, target string) (result coreruntime.CheckOutboundResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = coreruntime.CheckOutboundResult{Error: coreruntime.CheckOutboundErrorFailed}
		}
	}()
	checkConfig, err := checkTempCoreConfig(db, connection)
	if err != nil {
		return coreruntime.CheckOutboundResult{Error: coreruntime.ClassifyOutboundCheckError(err)}
	}
	config, err := json.Marshal(map[string]any{
		"log": map[string]any{
			"disabled": true,
		},
		"outbounds": checkConfig.Outbounds,
	})
	if err != nil {
		return coreruntime.CheckOutboundResult{Error: coreruntime.ClassifyOutboundCheckError(err)}
	}
	instance := coreruntime.NewCore()
	defer func() {
		_ = instance.Stop()
	}()
	if err := instance.Start(config); err != nil {
		return coreruntime.CheckOutboundResult{Error: coreruntime.ClassifyOutboundCheckError(err)}
	}
	return checkTempCoreOutboundTags(ctx, instance, checkConfig.CheckTags, target)
}
func checkOutbounds(db *gorm.DB, connection RemoteOutboundConnection) ([]json.RawMessage, error) {
	checkConfig, err := checkTempCoreConfig(db, connection)
	if err != nil {
		return nil, err
	}
	return checkConfig.Outbounds, nil
}
func checkTempCoreConfig(db *gorm.DB, connection RemoteOutboundConnection) (tempCoreCheckConfig, error) {
	if db == nil || !remoteConnectionIsGroup(connection) || connection.SubscriptionId == 0 {
		outbounds, err := checkConnectionOutboundConfigs(connection, nil)
		if err != nil {
			return tempCoreCheckConfig{}, err
		}
		return tempCoreCheckConfig{
			Outbounds: outbounds,
			CheckTags: []string{connection.OutboundTag},
		}, nil
	}
	connections, err := groupCheckConnections(db, connection)
	if err != nil {
		return tempCoreCheckConfig{}, err
	}
	tagMap := remoteConnectionTagMap(connections)
	checkTags := groupCheckTags(connection, connections, tagMap)
	result := make([]json.RawMessage, 0, len(connections))
	for _, item := range connections {
		if len(checkTags) > 0 && remoteConnectionIsGroup(item) {
			continue
		}
		outbounds, err := checkConnectionOutboundConfigs(item, tagMap)
		if err != nil {
			return tempCoreCheckConfig{}, err
		}
		result = append(result, outbounds...)
	}
	return tempCoreCheckConfig{
		Outbounds: result,
		CheckTags: checkTags,
	}, nil
}
func checkTempCoreOutboundTags(ctx context.Context, instance *coreruntime.Core, tags []string, target string) coreruntime.CheckOutboundResult {
	if ctx == nil {
		ctx = context.Background()
	}
	tags = uniqueCheckTags(tags)
	if len(tags) == 0 {
		return coreruntime.CheckOutboundResult{Error: coreruntime.CheckOutboundErrorInvalidRequest}
	}
	if len(tags) == 1 {
		return instance.CheckOutbound(ctx, tags[0], target)
	}

	results := make([]coreruntime.CheckOutboundResult, len(tags))
	workerCount := min(checkConcurrency, len(tags))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					results[index] = coreruntime.CheckOutboundResult{Error: coreruntime.ClassifyOutboundCheckError(err)}
					continue
				}
				results[index] = instance.CheckOutbound(ctx, tags[index], target)
			}
		}()
	}
	for index := range tags {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	var best coreruntime.CheckOutboundResult
	for _, result := range results {
		if result.OK {
			if !best.OK || result.Delay < best.Delay {
				best = result
			}
		}
	}
	if best.OK {
		return best
	}
	return coreruntime.CheckOutboundResult{Error: "outbound_group_failed"}
}
func uniqueCheckTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}
func checkConnectionOutboundConfigs(connection RemoteOutboundConnection, tagMap map[string]string) ([]json.RawMessage, error) {
	outbound, err := ConnectionOutboundConfig(connection)
	if err != nil {
		return nil, err
	}
	if tagMap != nil {
		outbound, err = connectionOutboundConfig(connection, tagMap)
	}
	if err != nil {
		return nil, err
	}
	if connection.Type != entityoutbounds.FailoverType {
		return []json.RawMessage{outbound}, nil
	}
	var options json.RawMessage
	if err := json.Unmarshal(outbound, &options); err != nil {
		return nil, err
	}
	panelOutbound := model.Outbound{
		Type:    connection.Type,
		Tag:     connection.OutboundTag,
		Options: connection.Options,
	}
	if tagMap != nil {
		payload := map[string]any{}
		if err := json.Unmarshal(outbound, &payload); err != nil {
			return nil, err
		}
		delete(payload, "type")
		delete(payload, "tag")
		options, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		panelOutbound.Options = options
	}
	return entityoutbounds.AssembleFailoverOutboundsForCore(panelOutbound, entityoutbounds.DirectTag)
}
