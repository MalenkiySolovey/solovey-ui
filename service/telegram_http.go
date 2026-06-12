package service

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
	"golang.org/x/net/proxy"
)

const telegramProxyDialTime = 10 * time.Second

var (
	telegramHTTPClientMu sync.RWMutex
	telegramHTTPClient   = &http.Client{Timeout: 10 * time.Second}
	telegramHTTPOverride bool
	telegramHTTPConfig   telegramProxyConfig
)

type telegramProxyConfig struct {
	URL      string
	Username string
	Password string
}

func (s *TelegramService) getTelegramHTTPClient() (*http.Client, error) {
	// A test override always wins (used by the test seam).
	telegramHTTPClientMu.RLock()
	if telegramHTTPOverride {
		client := telegramHTTPClient
		telegramHTTPClientMu.RUnlock()
		return client, nil
	}
	telegramHTTPClientMu.RUnlock()

	// Outbound transport: dial through a running sing-box outbound. Built fresh
	// each call (depends on the live core, which changes across restarts), so it
	// is not cached.
	if mode, _ := s.getString(settingKeyTelegramTransportMode); mode == "outbound" {
		tag, _ := s.getString(settingKeyTelegramOutboundTag)
		return newCoreOutboundHTTPClient(tag, 10*time.Second)
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
	if telegramHTTPOverride {
		return telegramHTTPClient, nil
	}
	if telegramHTTPClient != nil && telegramHTTPConfig == cfg {
		return telegramHTTPClient, nil
	}

	client, err := newTelegramHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	telegramHTTPClient = client
	telegramHTTPConfig = cfg
	return client, nil
}

func setTelegramHTTPClient(client *http.Client) func() {
	telegramHTTPClientMu.Lock()
	oldClient := telegramHTTPClient
	oldOverride := telegramHTTPOverride
	oldConfig := telegramHTTPConfig
	telegramHTTPClient = client
	telegramHTTPOverride = true
	telegramHTTPClientMu.Unlock()
	return func() {
		telegramHTTPClientMu.Lock()
		telegramHTTPClient = oldClient
		telegramHTTPOverride = oldOverride
		telegramHTTPConfig = oldConfig
		telegramHTTPClientMu.Unlock()
	}
}

func newTelegramHTTPClient(cfg telegramProxyConfig) (*http.Client, error) {
	if cfg.URL == "" {
		return &http.Client{Timeout: 10 * time.Second}, nil
	}
	if err := validateTelegramProxyURL(cfg.URL); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.Username != "" || cfg.Password != "" {
		parsed.User = url.UserPassword(cfg.Username, cfg.Password)
	}
	switch parsed.Scheme {
	case "http", "https":
		return &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyURL(parsed),
			},
		}, nil
	case "socks5":
		var auth *proxy.Auth
		username := cfg.Username
		password := cfg.Password
		if parsed.User != nil && username == "" && password == "" {
			username = parsed.User.Username()
			password, _ = parsed.User.Password()
		}
		if username != "" || password != "" {
			auth = &proxy.Auth{User: username, Password: password}
		}
		transport, err := newTelegramSOCKS5Transport(parsed.Host, auth)
		if err != nil {
			return nil, err
		}
		return &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		}, nil
	default:
		return nil, common.NewError("unsupported telegram proxy scheme")
	}
}

func newTelegramSOCKS5Transport(proxyHost string, auth *proxy.Auth) (*http.Transport, error) {
	forward := &net.Dialer{Timeout: telegramProxyDialTime}
	dialer, err := proxy.SOCKS5("tcp", proxyHost, auth, forward)
	if err != nil {
		return nil, err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, common.NewError("telegram socks5 proxy does not support context dial")
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialCtx, cancel := context.WithTimeout(ctx, telegramProxyDialTime)
			defer cancel()
			return contextDialer.DialContext(dialCtx, network, address)
		},
	}, nil
}

func validateTelegramProxyURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	if parsed, err := url.Parse(rawURL); err == nil && parsed.User != nil {
		return common.NewError("proxy url must not contain credentials; use the username/password fields")
	}
	return ssrf.ValidateOutboundURL(context.Background(), rawURL, "http", "https", "socks5")
}
