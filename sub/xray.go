package sub

import (
	"strconv"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type XrayService struct {
	service.SettingService
}

func (s *XrayService) GetXray(subID string) (*string, []string, error) {
	now := time.Now()
	enabled, err := s.SettingService.GetSubXrayEnable()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, common.NewError("xray subscription disabled")
	}
	cacheKey := "xray:" + subID + ":clientHooks=" + strconv.FormatUint(localsub.ClientOutboundContributorsVersion(), 10)
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}
	client, inbounds, err := loadClientData(subID)
	if err != nil {
		return nil, nil, err
	}
	outboundSet, err := localsub.BuildInboundOutbounds(client.Config, inbounds)
	if err != nil {
		return nil, nil, err
	}
	links := resolveClientLinks(client.Links, localsub.LinkModeExternal, "")
	localsub.AppendExternalLinkOutbounds(outboundSet, links)
	if err := localsub.AppendClientOutboundContributions(localsub.ClientOutboundContributionContext{
		DB:       dbsqlite.DB(),
		RawLinks: client.Links,
		Target:   subconversion.TargetXray,
	}, outboundSet); err != nil {
		return nil, nil, err
	}
	result, err := subformats.RenderXray(outboundSet.Outbounds)
	if err != nil {
		return nil, nil, err
	}
	displaySettings, err := subserver.CachedDisplaySettings(&s.SettingService, now)
	if err != nil {
		return nil, nil, err
	}
	headers := safeSubscriptionHeaders(buildClientHeaders(client, displaySettings))
	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}
