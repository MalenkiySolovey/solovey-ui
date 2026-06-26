package service

import (
	"net/http"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
)

// NewPaidSubHTTPClient builds the HTTP client the paid-subscriptions bot uses,
// honoring its own transport config (independent from the admin notifier):
// either paidSubProxy* or a sing-box outbound via paidSubOutboundTag.
// The longer timeout accommodates getUpdates long-polling.
func NewPaidSubHTTPClient(timeout time.Duration) (*http.Client, error) {
	s := &SettingService{}
	mode, _ := s.GetPaidSubTransportMode()
	if mode == "outbound" {
		tag, _ := s.GetPaidSubOutboundTag()
		return newCoreOutboundHTTPClient(tag, timeout)
	}
	cfg, err := s.paidSubProxyConfig()
	if err != nil {
		return nil, err
	}
	client, err := integrationtelegram.NewHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client, nil
}

func (s *SettingService) paidSubProxyConfig() (integrationtelegram.ProxyConfig, error) {
	proxyURL, err := s.getString(settingKeyPaidSubProxyURL)
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	username, err := s.getString(settingKeyPaidSubProxyUsername)
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	password, err := s.getString(settingKeyPaidSubProxyPassword)
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	return integrationtelegram.ProxyConfig{URL: proxyURL, Username: username, Password: password}, nil
}
