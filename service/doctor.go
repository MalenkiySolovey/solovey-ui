package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	opsdoctor "github.com/MalenkiySolovey/solovey-ui/internal/ops/doctor"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type DoctorService struct {
	Runtime *Runtime
}

type DoctorClientRequest struct {
	ClientID uint   `json:"clientId"`
	Target   string `json:"target,omitempty"`
}

func (s *DoctorService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *DoctorService) Run(hostname string) opsdoctor.Report {
	start := time.Now()
	var items []opsdoctor.Item
	configService := NewConfigServiceWithRuntime(s.runtime())
	serverService := NewServerService(s.runtime())

	rawConfig, err := configService.GetConfig("")
	if err != nil {
		items = append(items, opsdoctor.Error("config-build", "Build sing-box config", "Unable to build config: "+err.Error(), "Fix database/config rows before restarting sing-box.", nil))
		return opsdoctor.FinishReport(start, items)
	}
	items = append(items, opsdoctor.OK("config-build", "Build sing-box config", "Full sing-box config was assembled from database rows.", nil))

	if err := singboxvalidation.ValidateConfig(*rawConfig); err != nil {
		items = append(items, opsdoctor.Error("config-dry-check", "Dry config check", err.Error(), "Open the affected config section and fix the reported sing-box option.", nil))
	} else {
		items = append(items, opsdoctor.OK("config-dry-check", "Dry config check", "Config parses and constructs without starting sing-box.", nil))
	}

	if configService.IsCoreRunning() {
		items = append(items, opsdoctor.OK("core-running", "sing-box core", "Core is running.", nil))
	} else {
		items = append(items, opsdoctor.Warn("core-running", "sing-box core", "Core is not running.", "Start or restart sing-box after fixing config errors.", nil))
	}

	items = append(items, opsdoctor.ReferenceChecks(*rawConfig)...)
	items = append(items, s.subscriptionChecks(hostname)...)
	items = append(items, s.recentLogCheck(serverService))
	items = append(items, s.outboundChecks(configService))

	return opsdoctor.FinishReport(start, items)
}

func (s *DoctorService) DiagnoseClient(req DoctorClientRequest, hostname string) (opsdoctor.Report, error) {
	start := time.Now()
	if req.ClientID == 0 {
		return opsdoctor.Report{}, common.NewError("clientId is required")
	}
	db := dbsqlite.DB()
	if db == nil {
		return opsdoctor.Report{}, common.NewError("database is not initialized")
	}

	var client model.Client
	if err := db.Model(model.Client{}).Where("id = ?", req.ClientID).First(&client).Error; err != nil {
		return opsdoctor.Report{}, err
	}

	var items []opsdoctor.Item
	now := time.Now().Unix()
	if client.Enable {
		items = append(items, opsdoctor.OK("client-enabled", "Client enabled", "Client is enabled.", nil))
	} else {
		items = append(items, opsdoctor.Error("client-enabled", "Client enabled", "Client is disabled.", "Enable the client and save changes.", nil))
	}
	if client.Expiry == 0 || client.Expiry > now {
		items = append(items, opsdoctor.OK("client-expiry", "Expiry", "Client is not expired.", map[string]any{"expiry": client.Expiry}))
	} else {
		items = append(items, opsdoctor.Error("client-expiry", "Expiry", "Client is expired.", "Extend the client expiry date.", map[string]any{"expiry": client.Expiry}))
	}
	used := client.Up + client.Down
	if client.Volume == 0 || used < client.Volume {
		items = append(items, opsdoctor.OK("client-traffic", "Traffic limit", "Client traffic is within the configured limit.", map[string]any{"used": used, "volume": client.Volume}))
	} else {
		items = append(items, opsdoctor.Error("client-traffic", "Traffic limit", "Client reached the traffic limit.", "Increase volume or reset client traffic.", map[string]any{"used": used, "volume": client.Volume}))
	}

	inboundIDs, inboundItems := s.clientInboundChecks(client)
	items = append(items, inboundItems...)
	items = append(items, s.clientLinkChecks(client, inboundIDs, hostname)...)
	items = append(items, s.clientSubscriptionChecks(client)...)
	items = append(items, s.clientRuntimeChecks(client, req.Target)...)

	return opsdoctor.FinishReport(start, items), nil
}

func (s *DoctorService) subscriptionChecks(hostname string) []opsdoctor.Item {
	settingService := SettingService{}
	settings, err := settingService.GetAllSetting()
	if err != nil {
		return []opsdoctor.Item{opsdoctor.Error("subscription-settings", "Subscription settings", err.Error(), "Fix settings storage before serving subscriptions.", nil)}
	}
	var items []opsdoctor.Item
	subURI, err := settingService.GetFinalSubURI(hostname)
	if err != nil || strings.TrimSpace(subURI) == "" {
		items = append(items, opsdoctor.Warn("subscription-uri", "Subscription URI", "Subscription URI cannot be resolved.", "Set subscription domain/URI in Settings.", nil))
	} else {
		items = append(items, opsdoctor.OK("subscription-uri", "Subscription URI", "Subscription URI resolves to "+subURI, nil))
	}
	enabledFormats := 0
	for _, key := range []string{"subLinkEnable", "subJsonEnable", "subClashEnable", "subXrayEnable"} {
		if (*settings)[key] == "true" {
			enabledFormats++
		}
	}
	if enabledFormats == 0 {
		items = append(items, opsdoctor.Error("subscription-formats", "Subscription formats", "All subscription formats are disabled.", "Enable at least one subscription format.", nil))
	} else {
		items = append(items, opsdoctor.OK("subscription-formats", "Subscription formats", fmt.Sprintf("%d subscription format(s) enabled.", enabledFormats), nil))
	}
	if (*settings)["subSecretRequired"] != "true" {
		items = append(items, opsdoctor.Warn("subscription-secret", "Subscription secret mode", "Legacy name-based subscription lookup is still allowed.", "Enable required subscription secrets to prevent name guessing.", nil))
	} else {
		items = append(items, opsdoctor.OK("subscription-secret", "Subscription secret mode", "Per-client subscription secrets are required.", nil))
	}
	return items
}

func (s *DoctorService) recentLogCheck(serverService ServerService) opsdoctor.Item {
	logs, err := serverService.GetLogsFiltered("20", "warning", "core", "")
	if err != nil {
		return opsdoctor.Warn("core-logs", "Recent core logs", err.Error(), "Open Logs for details.", nil)
	}
	if len(logs) == 0 {
		return opsdoctor.OK("core-logs", "Recent core logs", "No recent core warnings/errors in the in-memory log buffer.", nil)
	}
	return opsdoctor.Warn("core-logs", "Recent core logs", "Recent core warnings/errors were found.", "Open Logs and inspect core warnings/errors.", firstNStrings(logs, 5))
}

func firstNStrings(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func (s *DoctorService) outboundChecks(configService *ConfigService) opsdoctor.Item {
	return s.outboundChecksTarget(configService, "https://www.gstatic.com/generate_204")
}

func (s *DoctorService) outboundChecksTarget(configService *ConfigService, target string) opsdoctor.Item {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "https://www.gstatic.com/generate_204"
	}
	outbounds, err := configService.OutboundService.GetAll()
	if err != nil {
		return opsdoctor.Warn("outbound-checks", "Outbound checks", err.Error(), "Open Outbounds and verify rows.", nil)
	}
	if !configService.IsCoreRunning() {
		return opsdoctor.Warn("outbound-checks", "Outbound checks", "Skipped because sing-box core is not running.", "Start sing-box before testing outbound latency.", nil)
	}
	tags := make([]string, 0, len(*outbounds))
	for _, outbound := range *outbounds {
		if tag, _ := outbound["tag"].(string); tag != "" {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	if len(tags) == 0 {
		return opsdoctor.Warn("outbound-checks", "Outbound checks", "No outbounds are configured.", "Add at least one proxy outbound for split routing.", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type result struct {
		Tag     string `json:"tag"`
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
		Delay   uint16 `json:"delay,omitempty"`
		Skipped bool   `json:"skipped,omitempty"`
	}
	results := make([]result, len(tags))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, tag := range tags {
		wg.Add(1)
		go func(index int, outboundTag string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index] = result{Tag: outboundTag, Error: ctx.Err().Error(), Skipped: true}
				return
			}
			check := configService.CheckOutboundWithContext(ctx, outboundTag, target)
			res := result{Tag: outboundTag, OK: check.OK, Error: check.Error, Delay: check.Delay}
			// A probe cancelled by the doctor's own time budget is "not tested",
			// not a genuine outbound failure — don't count it as failed.
			if !check.OK && ctx.Err() != nil {
				res.Skipped = true
			}
			results[index] = res
		}(i, tag)
	}
	wg.Wait()
	failed := 0
	skipped := 0
	for _, res := range results {
		switch {
		case res.Skipped:
			skipped++
		case !res.OK:
			failed++
		}
	}
	if failed > 0 {
		msg := fmt.Sprintf("%d outbound check(s) failed for %s.", failed, target)
		if skipped > 0 {
			msg += fmt.Sprintf(" %d not tested (time budget).", skipped)
		}
		return opsdoctor.Warn("outbound-checks", "Outbound checks", msg, "Open Outbounds and test failing tags individually.", results)
	}
	if skipped > 0 {
		return opsdoctor.Warn("outbound-checks", "Outbound checks", fmt.Sprintf("%d outbound(s) reached %s; %d not tested (time budget).", len(results)-skipped, target, skipped), "Re-run the doctor or test the remaining tags individually.", results)
	}
	return opsdoctor.OK("outbound-checks", "Outbound checks", fmt.Sprintf("%d outbound(s) reached %s.", len(results), target), results)
}

func (s *DoctorService) clientInboundChecks(client model.Client) ([]uint, []opsdoctor.Item) {
	var inboundIDs []uint
	if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
		return nil, []opsdoctor.Item{opsdoctor.Error("client-inbounds", "Client inbounds", "Client inbound list is malformed: "+err.Error(), "Re-save the client inbound list.", nil)}
	}
	if len(inboundIDs) == 0 {
		return inboundIDs, []opsdoctor.Item{opsdoctor.Error("client-inbounds", "Client inbounds", "Client has no inbounds.", "Assign at least one inbound to the client.", nil)}
	}
	var found []uint
	if err := dbsqlite.DB().Model(model.Inbound{}).Where("id in ?", inboundIDs).Pluck("id", &found).Error; err != nil {
		return inboundIDs, []opsdoctor.Item{opsdoctor.Error("client-inbounds", "Client inbounds", err.Error(), "Open Clients and re-save inbound membership.", nil)}
	}
	if len(found) != len(inboundIDs) {
		return inboundIDs, []opsdoctor.Item{opsdoctor.Error("client-inbounds", "Client inbounds", "Some assigned inbounds no longer exist.", "Remove stale inbound ids or assign valid inbounds.", map[string]any{"assigned": inboundIDs, "found": found})}
	}
	return inboundIDs, []opsdoctor.Item{opsdoctor.OK("client-inbounds", "Client inbounds", fmt.Sprintf("%d inbound(s) assigned.", len(inboundIDs)), inboundIDs)}
}

func (s *DoctorService) clientLinkChecks(client model.Client, inboundIDs []uint, hostname string) []opsdoctor.Item {
	var links []map[string]string
	if len(strings.TrimSpace(string(client.Links))) > 0 {
		if err := json.Unmarshal(client.Links, &links); err != nil {
			return []opsdoctor.Item{opsdoctor.Error("client-links", "Client links", "Client links are malformed: "+err.Error(), "Re-save the client to rebuild generated links.", nil)}
		}
	}
	if len(links) == 0 && len(inboundIDs) > 0 {
		return []opsdoctor.Item{opsdoctor.Warn("client-links", "Client links", "No generated links are stored for this client.", "Re-save the client or its inbounds to regenerate links.", nil)}
	}
	return []opsdoctor.Item{opsdoctor.OK("client-links", "Client links", fmt.Sprintf("%d link(s) stored for delivery.", len(links)), nil)}
}

func (s *DoctorService) clientSubscriptionChecks(client model.Client) []opsdoctor.Item {
	settingService := SettingService{}
	var items []opsdoctor.Item
	required, reqErr := settingService.GetSubSecretRequired()
	if client.SubSecret == "" {
		items = append(items, opsdoctor.Warn("client-sub-secret", "Subscription secret", "Client has no subscription secret yet.", "Rotate/re-save the client to generate a secret.", nil))
	} else {
		items = append(items, opsdoctor.OK("client-sub-secret", "Subscription secret", "Client has a subscription secret.", nil))
	}
	if reqErr != nil {
		items = append(items, opsdoctor.Warn("client-sub-secret-required", "Subscription lookup", "Could not read the subscription secret requirement: "+reqErr.Error(), "Check settings storage.", nil))
	} else if !required {
		items = append(items, opsdoctor.Warn("client-sub-secret-required", "Subscription lookup", "Legacy name lookup is allowed globally.", "Enable required subscription secrets in Settings.", nil))
	}
	linkOn, linkErr := settingService.GetSubLinkEnable()
	jsonOn, jsonErr := settingService.GetSubJsonEnable()
	clashOn, clashErr := settingService.GetSubClashEnable()
	xrayOn, xrayErr := settingService.GetSubXrayEnable()
	if linkErr != nil || jsonErr != nil || clashErr != nil || xrayErr != nil {
		// Do not assert "all formats disabled" when the settings read itself failed;
		// that would point the operator at the wrong fix.
		items = append(items, opsdoctor.Warn("client-sub-formats", "Subscription formats", "Could not read subscription format settings.", "Check settings storage before serving subscriptions.", nil))
		return items
	}
	enabled := map[string]bool{"link": linkOn, "json": jsonOn, "clash": clashOn, "xray": xrayOn}
	count := 0
	for _, ok := range enabled {
		if ok {
			count++
		}
	}
	if count == 0 {
		items = append(items, opsdoctor.Error("client-sub-formats", "Subscription formats", "All subscription formats are disabled.", "Enable at least one subscription format.", enabled))
	} else {
		items = append(items, opsdoctor.OK("client-sub-formats", "Subscription formats", fmt.Sprintf("%d format(s) enabled.", count), enabled))
	}
	return items
}

func (s *DoctorService) clientRuntimeChecks(client model.Client, target string) []opsdoctor.Item {
	configService := NewConfigServiceWithRuntime(s.runtime())
	var items []opsdoctor.Item
	if configService.IsCoreRunning() {
		items = append(items, opsdoctor.OK("client-core", "sing-box core", "Core is running.", nil))
	} else {
		items = append(items, opsdoctor.Warn("client-core", "sing-box core", "Core is not running.", "Start sing-box before testing client traffic.", nil))
	}
	statsService := StatsService{Runtime: s.runtime()}
	onlines, err := statsService.GetOnlines()
	if err == nil {
		if doctorContainsString(onlines.User, client.Name) {
			items = append(items, opsdoctor.OK("client-online", "Online signal", "Client is currently online.", nil))
		} else {
			items = append(items, opsdoctor.Warn("client-online", "Online signal", "Client is not currently reported online.", "Ask the user to reconnect, then refresh online status.", map[string]any{"lastOnline": client.LastOnline, "lastIpCount": client.LastIPCount}))
		}
	}
	items = append(items, s.outboundChecksTarget(configService, target))
	return items
}

func doctorContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
