package sub

import (
	subconversion "github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type JSONService struct{ service.SettingService }

func (s *JSONService) GetJSON(id string) (*string, []string, error) {
	return publisherFor(&s.SettingService).GetJSON(id)
}
func (s *JSONService) render(client *model.Client, inbounds []*model.Inbound) (string, error) {
	outboundSet, err := localsub.BuildInboundOutbounds(client.Config, inbounds)
	if err != nil {
		return "", err
	}
	links := resolveClientLinks(client.Links, localsub.LinkModeExternal, "")
	localsub.AppendExternalLinkOutbounds(outboundSet, links)
	if err := localsub.AppendClientOutboundContributions(localsub.ClientOutboundContributionContext{
		DB:       dbsqlite.DB(),
		RawLinks: client.Links,
		Target:   subconversion.TargetSingBox,
	}, outboundSet); err != nil {
		return "", err
	}
	localsub.PrependDefaultJSONOutbounds(outboundSet)

	options, err := s.jsonOptions()
	if err != nil {
		return "", err
	}
	result, err := subformats.RenderJSON(outboundSet.Outbounds, options)
	if err != nil {
		return "", err
	}

	return result, nil
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
