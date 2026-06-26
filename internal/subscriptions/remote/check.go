package remote

import (
	"context"
	"encoding/json"
	"fmt"
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
	var connection model.RemoteOutboundConnection
	if err := db.First(&connection, id).Error; err != nil {
		return nil, err
	}
	return CheckConnectionRecordWithDB(ctx, db, connection, target), nil
}
func CheckSubscription(ctx context.Context, db *gorm.DB, subscriptionID uint, target string) ([]CheckResult, error) {
	var connections []model.RemoteOutboundConnection
	if err := db.
		Where("subscription_id = ?", subscriptionID).
		Order(entityorder.Clause).
		Find(&connections).Error; err != nil {
		return nil, err
	}
	return CheckConnectionRecordsWithDB(ctx, db, connections, target), nil
}
func CheckAll(ctx context.Context, db *gorm.DB, target string) ([]CheckResult, error) {
	var connections []model.RemoteOutboundConnection
	if err := db.
		Where("enabled = ?", true).
		Order("subscription_id ASC, sort_order ASC, id ASC").
		Find(&connections).Error; err != nil {
		return nil, err
	}
	return CheckConnectionRecordsWithDB(ctx, db, connections, target), nil
}
func CheckConnectionRecords(ctx context.Context, connections []model.RemoteOutboundConnection, target string) []CheckResult {
	return CheckConnectionRecordsWithDB(ctx, nil, connections, target)
}
func CheckConnectionRecordsWithDB(ctx context.Context, db *gorm.DB, connections []model.RemoteOutboundConnection, target string) []CheckResult {
	results := make([]CheckResult, len(connections))
	sem := make(chan struct{}, checkConcurrency)
	var wg sync.WaitGroup

	for i, connection := range connections {
		wg.Add(1)
		go func(index int, item model.RemoteOutboundConnection) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index] = CheckResult{
					ConnectionId: item.Id,
					OutboundTag:  item.OutboundTag,
					Error:        ctx.Err().Error(),
				}
				return
			}

			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			results[index] = *CheckConnectionRecordWithDB(checkCtx, db, item, target)
		}(i, connection)
	}

	wg.Wait()
	return results
}
func CheckConnectionRecord(ctx context.Context, connection model.RemoteOutboundConnection, target string) *CheckResult {
	return CheckConnectionRecordWithDB(ctx, nil, connection, target)
}
func CheckConnectionRecordWithDB(ctx context.Context, db *gorm.DB, connection model.RemoteOutboundConnection, target string) *CheckResult {
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
func CheckConnectionWithTempCore(ctx context.Context, connection model.RemoteOutboundConnection, target string) coreruntime.CheckOutboundResult {
	return CheckConnectionWithTempCoreDB(ctx, nil, connection, target)
}
func CheckConnectionWithTempCoreDB(ctx context.Context, db *gorm.DB, connection model.RemoteOutboundConnection, target string) (result coreruntime.CheckOutboundResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = coreruntime.CheckOutboundResult{Error: fmt.Sprintf("temporary core check failed: %v", recovered)}
		}
	}()
	checkConfig, err := checkTempCoreConfig(db, connection)
	if err != nil {
		return coreruntime.CheckOutboundResult{Error: err.Error()}
	}
	config, err := json.Marshal(map[string]any{
		"log": map[string]any{
			"disabled": true,
		},
		"outbounds": checkConfig.Outbounds,
	})
	if err != nil {
		return coreruntime.CheckOutboundResult{Error: err.Error()}
	}
	instance := coreruntime.NewCore()
	defer func() {
		_ = instance.Stop()
	}()
	if err := instance.Start(config); err != nil {
		return coreruntime.CheckOutboundResult{Error: err.Error()}
	}
	return checkTempCoreOutboundTags(ctx, instance, checkConfig.CheckTags, target)
}
func checkOutbounds(db *gorm.DB, connection model.RemoteOutboundConnection) ([]json.RawMessage, error) {
	checkConfig, err := checkTempCoreConfig(db, connection)
	if err != nil {
		return nil, err
	}
	return checkConfig.Outbounds, nil
}
func checkTempCoreConfig(db *gorm.DB, connection model.RemoteOutboundConnection) (tempCoreCheckConfig, error) {
	if db == nil || !remoteConnectionIsGroup(connection) || connection.SubscriptionId == 0 {
		outbound, err := checkConnectionOutboundConfig(connection, nil)
		if err != nil {
			return tempCoreCheckConfig{}, err
		}
		return tempCoreCheckConfig{
			Outbounds: []json.RawMessage{outbound},
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
		outbound, err := checkConnectionOutboundConfig(item, tagMap)
		if err != nil {
			return tempCoreCheckConfig{}, err
		}
		result = append(result, outbound)
	}
	return tempCoreCheckConfig{
		Outbounds: result,
		CheckTags: checkTags,
	}, nil
}
func checkTempCoreOutboundTags(ctx context.Context, instance *coreruntime.Core, tags []string, target string) coreruntime.CheckOutboundResult {
	tags = uniqueCheckTags(tags)
	if len(tags) == 0 {
		return coreruntime.CheckOutboundResult{Error: "outbound tag is empty"}
	}
	if len(tags) == 1 {
		return instance.CheckOutbound(ctx, tags[0], target)
	}

	type tagResult struct {
		tag    string
		result coreruntime.CheckOutboundResult
	}
	results := make(chan tagResult, len(tags))
	sem := make(chan struct{}, checkConcurrency)
	var wg sync.WaitGroup
	for _, tag := range tags {
		wg.Add(1)
		go func(outboundTag string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- tagResult{tag: outboundTag, result: coreruntime.CheckOutboundResult{Error: ctx.Err().Error()}}
				return
			}
			results <- tagResult{tag: outboundTag, result: instance.CheckOutbound(ctx, outboundTag, target)}
		}(tag)
	}
	wg.Wait()
	close(results)

	var best coreruntime.CheckOutboundResult
	errors := make([]string, 0, len(tags))
	for item := range results {
		if item.result.OK {
			if !best.OK || item.result.Delay < best.Delay {
				best = item.result
			}
			continue
		}
		if message := strings.TrimSpace(item.result.Error); message != "" {
			errors = append(errors, fmt.Sprintf("%s: %s", item.tag, message))
		}
	}
	if best.OK {
		return best
	}
	if len(errors) == 0 {
		return coreruntime.CheckOutboundResult{Error: "all group members failed"}
	}
	return coreruntime.CheckOutboundResult{Error: "all group members failed: " + strings.Join(errors, "; ")}
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
func checkConnectionOutboundConfig(connection model.RemoteOutboundConnection, tagMap map[string]string) (json.RawMessage, error) {
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
		return outbound, nil
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
	return entityoutbounds.AssembleFailoverForCore(panelOutbound, "")
}
