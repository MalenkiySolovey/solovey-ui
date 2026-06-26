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

type JSONService struct {
	service.SettingService
}

func (s *JSONService) GetJSON(subID string) (*string, []string, error) {
	now := time.Now()
	cacheKey := "json:" + subID
	if body, headers, ok := subscriptionCacheGet(cacheKey, now); ok {
		return &body, headers, nil
	}

	enabled, err := s.SettingService.GetSubJsonEnable()
	if err == nil && !enabled {
		return nil, nil, common.NewError("json subscription disabled")
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
	if err := localsub.AppendRemoteGroupOutboundsWithOptions(dbsqlite.DB(), outboundSet, client.Links, remoteClientConversionOptions(&s.SettingService, subconversion.TargetSingBox)); err != nil {
		return nil, nil, err
	}
	localsub.PrependDefaultJSONOutbounds(outboundSet)

	options, err := s.jsonOptions()
	if err != nil {
		return nil, nil, err
	}
	result, err := subformats.RenderJSON(outboundSet.Outbounds, options)
	if err != nil {
		return nil, nil, err
	}
	headers := safeSubscriptionHeaders(buildClientHeaders(client, subserver.CachedDisplaySettings(&s.SettingService, now)))
	subscriptionCacheSet(cacheKey, result, headers, now)
	return &result, headers, nil
}

func (s *JSONService) jsonOptions() (subformats.JSONOptions, error) {
	var options subformats.JSONOptions
	var err error
	if options.Extension, err = s.SettingService.GetSubJsonExt(); err != nil {
		return options, err
	}
	if options.DirectRules, err = s.SettingService.GetSubJsonDirectRules(); err != nil {
		return options, err
	}
	if options.Mux, err = s.SettingService.GetSubJsonMux(); err != nil {
		return options, err
	}
	if options.Noises, err = s.SettingService.GetSubJsonNoises(); err != nil {
		return options, err
	}
	if options.Fragment, err = s.SettingService.GetSubJsonFragment(); err != nil {
		return options, err
	}
	return options, nil
}
