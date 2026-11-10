//go:build !minimal

package telegram

import (
	"errors"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
)

func (s *Service) telegramProxyConfig() (integrationtelegram.ProxyConfig, error) {
	if s == nil || s.Settings == nil {
		return integrationtelegram.ProxyConfig{}, errors.New("telegram settings are unavailable")
	}
	proxyURL, err := s.Settings.GetTelegramProxyURL()
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	username, err := s.Settings.GetTelegramProxyUsername()
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	password, err := s.Settings.GetTelegramProxyPassword()
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	return integrationtelegram.ProxyConfig{
		URL:      proxyURL,
		Username: username,
		Password: password,
	}, nil
}
