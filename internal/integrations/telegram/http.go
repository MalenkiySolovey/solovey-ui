package telegram

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
	"golang.org/x/net/proxy"
)

const ProxyDialTime = 10 * time.Second

type ProxyConfig struct {
	URL      string
	Username string
	Password string
}

func NewHTTPClient(cfg ProxyConfig) (*http.Client, error) {
	if cfg.URL == "" {
		return &http.Client{Timeout: 10 * time.Second}, nil
	}
	if err := ValidateProxyURL(cfg.URL); err != nil {
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
		transport, err := NewSOCKS5Transport(parsed.Host, auth)
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

func NewSOCKS5Transport(proxyHost string, auth *proxy.Auth) (*http.Transport, error) {
	forward := &net.Dialer{Timeout: ProxyDialTime}
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
			dialCtx, cancel := context.WithTimeout(ctx, ProxyDialTime)
			defer cancel()
			return contextDialer.DialContext(dialCtx, network, address)
		},
	}, nil
}

func ValidateProxyURL(rawURL string) error {
	return ssrf.ValidateProxyURL(rawURL)
}
