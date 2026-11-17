package sub

import (
	subconversion "github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type ClashService struct{ service.SettingService }

func (s *ClashService) GetClash(id string) (*string, []string, error) {
	return publisherFor(&s.SettingService).GetClash(id)
}
func (s *ClashService) render(client *model.Client, inbounds []*model.Inbound) (string, error) {
	outboundSet, err := localsub.BuildInboundOutbounds(client.Config, inbounds)
	if err != nil {
		return "", err
	}
	links := resolveClientLinks(client.Links, localsub.LinkModeExternal, "")
	localsub.AppendExternalLinkOutbounds(outboundSet, links)
	if err := localsub.AppendClientOutboundContributions(localsub.ClientOutboundContributionContext{
		DB:       dbsqlite.DB(),
		RawLinks: client.Links,
		Target:   subconversion.TargetMihomo,
	}, outboundSet); err != nil {
		return "", err
	}

	basicConfig, err := s.SettingService.GetSubClashExt()
	if err != nil || basicConfig == "" {
		basicConfig = subformats.DefaultClashConfig
	}
	udpValue, err := s.SettingService.GetSubClashUDP()
	if err != nil {
		return "", err
	}
	udpPolicy, err := subformats.ParseClashUDPPolicy(udpValue)
	if err != nil {
		return "", err
	}
	result, err := subformats.RenderClash(outboundSet.Outbounds, basicConfig, udpPolicy)
	if err != nil {
		return "", err
	}

	return result, nil
}
