//go:build !minimal

package telegram

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
)

var (
	telegramHTTPClientMu sync.RWMutex
	telegramHTTPClient   *http.Client
	telegramHTTPConfig   integrationtelegram.ProxyConfig
)

func (s *Service) HTTPClient() (*http.Client, error) {
	client, _, err := s.httpClient()
	return client, err
}

func (s *Service) httpClient() (*http.Client, bool, error) {
	if s == nil {
		return nil, false, fmt.Errorf("telegram service is unavailable")
	}
	if s.Client != nil {
		return s.Client, false, nil
	}
	if s.Settings == nil {
		return nil, false, fmt.Errorf("telegram settings are unavailable")
	}

	// Outbound transport: dial through a running sing-box outbound. Built fresh
	// each call (depends on the live core, which changes across restarts), so it
	// is not cached.
	mode, err := s.Settings.GetTelegramTransportMode()
	if err != nil {
		return nil, false, fmt.Errorf("read telegram transport mode: %w", err)
	}
	if mode == "outbound" {
		tag, err := s.Settings.GetTelegramOutboundTag()
		if err != nil {
			return nil, false, fmt.Errorf("read telegram outbound tag: %w", err)
		}
		if s.Runtime == nil {
			return nil, false, fmt.Errorf("telegram runtime is not configured")
		}
		client, err := s.Runtime.CoreHTTPClient(tag, 10*time.Second)
		return client, err == nil && client != nil, err
	}

	cfg, err := s.telegramProxyConfig()
	if err != nil {
		return nil, false, err
	}
	telegramHTTPClientMu.RLock()
	if telegramHTTPClient != nil && telegramHTTPConfig == cfg {
		client := telegramHTTPClient
		telegramHTTPClientMu.RUnlock()
		return client, false, nil
	}
	telegramHTTPClientMu.RUnlock()

	telegramHTTPClientMu.Lock()
	defer telegramHTTPClientMu.Unlock()
	if telegramHTTPClient != nil && telegramHTTPConfig == cfg {
		return telegramHTTPClient, false, nil
	}

	client, err := integrationtelegram.NewHTTPClient(cfg)
	if err != nil {
		return nil, false, err
	}
	if client.Transport == nil {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			client.Transport = transport.Clone()
		}
	}
	previous := telegramHTTPClient
	telegramHTTPClient = client
	telegramHTTPConfig = cfg
	closeTelegramIdleConnections(previous)
	return client, false, nil
}

// ResetHTTPClient releases component-owned idle HTTP connections on lifecycle stop.
func ResetHTTPClient() {
	telegramHTTPClientMu.Lock()
	previous := telegramHTTPClient
	telegramHTTPClient = nil
	telegramHTTPConfig = integrationtelegram.ProxyConfig{}
	telegramHTTPClientMu.Unlock()
	closeTelegramIdleConnections(previous)
}

func closeTelegramIdleConnections(client *http.Client) {
	if client != nil {
		client.CloseIdleConnections()
	}
}
