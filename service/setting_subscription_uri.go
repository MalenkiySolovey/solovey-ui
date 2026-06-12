package service

import (
	"net"
	"strings"
)

func (s *SettingService) GetFinalSubURI(host string) (string, error) {
	settings, err := s.getSubscriptionEndpointSettings()
	if err != nil {
		return "", err
	}
	if settings.OverrideURI != "" {
		return settings.OverrideURI, nil
	}
	return settings.BaseURI(host), nil
}

type subscriptionEndpointSettings struct {
	OverrideURI string
	Domain      string
	Port        string
	CertFile    string
	KeyFile     string
	Path        string
}

func (s *SettingService) getSubscriptionEndpointSettings() (subscriptionEndpointSettings, error) {
	overrideURI, err := s.GetSubURI()
	if err != nil {
		return subscriptionEndpointSettings{}, err
	}
	domain, err := s.GetSubDomain()
	if err != nil {
		return subscriptionEndpointSettings{}, err
	}
	port, err := s.getString(settingKeySubPort)
	if err != nil {
		return subscriptionEndpointSettings{}, err
	}
	certFile, err := s.GetSubCertFile()
	if err != nil {
		return subscriptionEndpointSettings{}, err
	}
	keyFile, err := s.GetSubKeyFile()
	if err != nil {
		return subscriptionEndpointSettings{}, err
	}
	path, err := s.GetSubPath()
	if err != nil {
		return subscriptionEndpointSettings{}, err
	}
	return subscriptionEndpointSettings{
		OverrideURI: overrideURI,
		Domain:      domain,
		Port:        port,
		CertFile:    certFile,
		KeyFile:     keyFile,
		Path:        path,
	}, nil
}

func (s subscriptionEndpointSettings) BaseURI(host string) string {
	protocol := "http"
	if s.KeyFile != "" && s.CertFile != "" {
		protocol = "https"
	}
	if s.Domain != "" {
		host = s.Domain
	}
	port := s.Port
	authority := hostForURL(host)
	if (port == "80" && protocol == "http") || (port == "443" && protocol == "https") {
		port = ""
	}
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}
	return protocol + "://" + authority + s.Path
}

func hostForURL(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}
