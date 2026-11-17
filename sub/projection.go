package sub

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	canonical "github.com/MalenkiySolovey/solovey-backend/subscriptions"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type subscriptionProjection struct{ settings *service.SettingService }

func publisherFor(s *service.SettingService) canonical.Publisher {
	return canonical.Publisher{Settings: s, Projection: subscriptionProjection{settings: s}, Warning: logger.Warning}
}
func (subscriptionProjection) PrivateLinks(c *model.Client) (*model.Client, error) {
	return privateLinkDelivery(c)
}
func (subscriptionProjection) LoadDelivery(c *model.Client) (*model.Client, []*model.Inbound, error) {
	return loadDelivery(c)
}
func (subscriptionProjection) ResolveLinks(raw json.RawMessage, info string) []string {
	return resolveClientLinks(raw, localsub.LinkModeAll, info)
}
func (p subscriptionProjection) RenderJSON(c *model.Client, inbounds []*model.Inbound) (string, error) {
	return (&JSONService{SettingService: *p.settings}).render(c, inbounds)
}
func (p subscriptionProjection) RenderClash(c *model.Client, inbounds []*model.Inbound) (string, error) {
	return (&ClashService{SettingService: *p.settings}).render(c, inbounds)
}
func (p subscriptionProjection) RenderXray(c *model.Client, inbounds []*model.Inbound) (string, error) {
	return (&XrayService{SettingService: *p.settings}).render(c, inbounds)
}
func (subscriptionProjection) ContributorsVersion() uint64 {
	return localsub.ClientOutboundContributorsVersion()
}
