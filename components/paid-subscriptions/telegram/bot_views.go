//go:build !minimal

package telegram

import (
	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/customerbot"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	sublocal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"gorm.io/gorm"
)

func (b *Bot) subscriptionFacts(db *gorm.DB, telegramID int64) (string, []string, error) {
	client, err := paidstore.ClientByTelegramUserID(db, telegramID)
	if err != nil {
		return "", nil, err
	}
	var url string
	if client.SubSecret != "" {
		host, _ := b.setting.GetWebDomain()
		if base, err := b.setting.GetFinalSubURI(host); err == nil && base != "" {
			url = base + client.SubSecret
		}
	}
	var links []string
	if len(client.Links) > 0 {
		if enabled, err := b.setting.GetSubLinkEnable(); err != nil || enabled {
			links = sublocal.ResolveClientLinks(client.Links, sublocal.LinkModeAll, "")
		}
	}
	return url, links, nil
}

func (b *Bot) usageFacts(db *gorm.DB, telegramID int64) (customerbot.Usage, error) {
	client, err := paidstore.ClientByTelegramUserID(db, telegramID)
	if err != nil {
		return customerbot.Usage{}, err
	}
	usage := customerbot.Usage{Used: client.Up + client.Down, Limit: client.Volume, Expiry: client.Expiry, Enabled: client.Enable}
	if online, err := b.stats.GetOnlines(); err == nil {
		for _, name := range online.User {
			if name == client.Name {
				usage.Online = true
				break
			}
		}
	}
	return usage, nil
}
