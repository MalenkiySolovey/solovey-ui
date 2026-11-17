package subscriptions

import (
	"errors"
	subserver "github.com/MalenkiySolovey/solovey-ui/backend/subscriptions/server"
	"strconv"
	"time"
)

func (p Publisher) GetJSON(subID string) (*string, []string, error) {
	if err := p.validate(); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	enabled, err := p.Settings.GetSubJsonEnable()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, errors.New("json subscription disabled")
	}
	cacheKey := "json:" + subID + ":clientHooks=" + strconv.FormatUint(p.Projection.ContributorsVersion(), 10)
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}
	client, inbounds, err := p.LoadClientData(subID)
	if err != nil {
		return nil, nil, err
	}
	defer clear(client.Config)
	result, err := p.Projection.RenderJSON(client, inbounds)
	if err != nil {
		return nil, nil, err
	}
	displaySettings, err := subserver.CachedDisplaySettings(p.Settings, now)
	if err != nil {
		return nil, nil, err
	}
	headers := safeSubscriptionHeaders(BuildClientHeaders(client, displaySettings))
	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}

func (p Publisher) GetClash(subID string) (*string, []string, error) {
	if err := p.validate(); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	enabled, err := p.Settings.GetSubClashEnable()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, errors.New("clash subscription disabled")
	}
	cacheKey := "clash:" + subID + ":clientHooks=" + strconv.FormatUint(p.Projection.ContributorsVersion(), 10)
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}

	client, inbounds, err := p.LoadClientData(subID)
	if err != nil {
		return nil, nil, err
	}
	defer clear(client.Config)
	result, err := p.Projection.RenderClash(client, inbounds)
	if err != nil {
		return nil, nil, err
	}
	displaySettings, err := subserver.CachedDisplaySettings(p.Settings, now)
	if err != nil {
		return nil, nil, err
	}
	headers := safeSubscriptionHeaders(BuildClientHeaders(client, displaySettings))
	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}

func (p Publisher) GetXray(subID string) (*string, []string, error) {
	if err := p.validate(); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	enabled, err := p.Settings.GetSubXrayEnable()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, errors.New("xray subscription disabled")
	}
	cacheKey := "xray:" + subID + ":clientHooks=" + strconv.FormatUint(p.Projection.ContributorsVersion(), 10)
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}
	client, inbounds, err := p.LoadClientData(subID)
	if err != nil {
		return nil, nil, err
	}
	defer clear(client.Config)
	result, err := p.Projection.RenderXray(client, inbounds)
	if err != nil {
		return nil, nil, err
	}
	displaySettings, err := subserver.CachedDisplaySettings(p.Settings, now)
	if err != nil {
		return nil, nil, err
	}
	headers := safeSubscriptionHeaders(BuildClientHeaders(client, displaySettings))
	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}
