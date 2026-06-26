package sub

import (
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
	cacheKey := "xray:" + subID
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}

	enabled, err := s.SettingService.GetSubXrayEnable()
	if err == nil && !enabled {
		return nil, nil, common.NewError("xray subscription disabled")
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
	if err := localsub.AppendRemoteGroupOutboundsWithOptions(dbsqlite.DB(), outboundSet, client.Links, remoteClientConversionOptions(&s.SettingService, subconversion.TargetXray)); err != nil {
		return nil, nil, err
	}
	result, err := subformats.RenderXray(outboundSet.Outbounds)
	if err != nil {
		return nil, nil, err
	}
	headers := safeSubscriptionHeaders(buildClientHeaders(client, subserver.CachedDisplaySettings(&s.SettingService, now)))
	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}
