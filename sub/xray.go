package sub

import (
	subconversion "github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type XrayService struct{ service.SettingService }

func (s *XrayService) GetXray(id string) (*string, []string, error) {
	return publisherFor(&s.SettingService).GetXray(id)
}
func (s *XrayService) render(client *model.Client, inbounds []*model.Inbound) (string, error) {
	outboundSet, err := localsub.BuildInboundOutbounds(client.Config, inbounds)
	if err != nil {
		return "", err
	}
	links := resolveClientLinks(client.Links, localsub.LinkModeExternal, "")
	localsub.AppendExternalLinkOutbounds(outboundSet, links)
	if err := localsub.AppendClientOutboundContributions(localsub.ClientOutboundContributionContext{
		DB:       dbsqlite.DB(),
		RawLinks: client.Links,
		Target:   subconversion.TargetXray,
	}, outboundSet); err != nil {
		return "", err
	}
	result, err := subformats.RenderXray(outboundSet.Outbounds)
	if err != nil {
		return "", err
	}

	return result, nil
}
