package telegram

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
)

var (
	telegramHTTPClientMu sync.RWMutex
	telegramHTTPClient   = &http.Client{Timeout: 10 * time.Second}
	telegramHTTPConfig   integrationtelegram.ProxyConfig
)

func (s *Service) HTTPClient() (*http.Client, error) {
	if s.Client != nil {
		return s.Client, nil
	}

	// Outbound transport: dial through a running sing-box outbound. Built fresh
	// each call (depends on the live core, which changes across restarts), so it
	// is not cached.
	if mode, _ := s.Settings.GetTelegramTransportMode(); mode == "outbound" {
		tag, _ := s.Settings.GetTelegramOutboundTag()
		if s.Runtime == nil {
			return nil, fmt.Errorf("telegram runtime is not configured")
		}
		return s.Runtime.CoreHTTPClient(tag, 10*time.Second)
	}

	cfg, err := s.telegramProxyConfig()
	if err != nil {
		return nil, err
	}
	telegramHTTPClientMu.RLock()
	if telegramHTTPClient != nil && telegramHTTPConfig == cfg {
		client := telegramHTTPClient
		telegramHTTPClientMu.RUnlock()
		return client, nil
	}
	telegramHTTPClientMu.RUnlock()

	telegramHTTPClientMu.Lock()
	defer telegramHTTPClientMu.Unlock()
	if telegramHTTPClient != nil && telegramHTTPConfig == cfg {
		return telegramHTTPClient, nil
	}

	client, err := integrationtelegram.NewHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	telegramHTTPClient = client
	telegramHTTPConfig = cfg
	return client, nil
}
