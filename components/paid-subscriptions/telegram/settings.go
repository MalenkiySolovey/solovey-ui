//go:build !minimal

package telegram

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type paidSettings struct {
	service.SettingService
}

func (s *paidSettings) GetPaidSubEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.EnabledKey)
}

func (s *paidSettings) GetPaidSubBotToken() (string, error) {
	return s.GetComponentSettingString(paidsettings.BotTokenKey)
}

func (s *paidSettings) GetPaidSubBotPollSeconds() (int, error) {
	v, err := s.GetComponentSettingInt(paidsettings.BotPollSecondsKey)
	if err != nil {
		return 25, err
	}
	if v < 1 {
		v = 1
	}
	if v > 50 {
		v = 50
	}
	return v, nil
}

func (s *paidSettings) GetPaidSubUpdateOffset() (int64, error) {
	str, err := s.GetComponentSettingString(paidsettings.UpdateOffsetKey)
	if err != nil {
		return 0, err
	}
	if str == "" {
		return 0, nil
	}
	return strconv.ParseInt(str, 10, 64)
}

func (s *paidSettings) SetPaidSubUpdateOffset(offset int64) error {
	return s.SetComponentSettingString(paidsettings.UpdateOffsetKey, strconv.FormatInt(offset, 10))
}

func (s *paidSettings) GetPaidSubAutoRegister() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.AutoRegisterKey)
}

func (s *paidSettings) GetPaidSubAutoInbounds() ([]uint, error) {
	str, err := s.GetComponentSettingString(paidsettings.AutoInboundsKey)
	if err != nil {
		return nil, err
	}
	if str == "" {
		return []uint{}, nil
	}
	var ids []uint
	if err := json.Unmarshal([]byte(str), &ids); err != nil {
		return []uint{}, nil
	}
	return ids, nil
}

func (s *paidSettings) GetPaidSubTrialDays() (int, error) {
	return s.GetComponentSettingInt(paidsettings.TrialDaysKey)
}

func (s *paidSettings) GetPaidSubTrialVolumeGB() (int, error) {
	return s.GetComponentSettingInt(paidsettings.TrialVolumeGBKey)
}

func (s *paidSettings) GetPaidSubMaxClients() (int, error) {
	return s.GetComponentSettingInt(paidsettings.MaxClientsKey)
}

func (s *paidSettings) GetPaidSubStartRateLimitPerMin() (int, error) {
	return s.GetComponentSettingInt(paidsettings.StartRateLimitPerMinKey)
}

func (s *paidSettings) GetPaidSubStarsEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.StarsEnabledKey)
}

func (s *paidSettings) GetPaidSubYooKassaEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.YooKassaEnabledKey)
}

func (s *paidSettings) GetPaidSubYooKassaToken() (string, error) {
	return s.GetComponentSettingString(paidsettings.YooKassaTokenKey)
}

func (s *paidSettings) GetPaidSubStripeEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.StripeEnabledKey)
}

func (s *paidSettings) GetPaidSubStripeToken() (string, error) {
	return s.GetComponentSettingString(paidsettings.StripeTokenKey)
}

func (s *paidSettings) GetPaidSubPayMasterEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.PayMasterEnabledKey)
}

func (s *paidSettings) GetPaidSubPayMasterToken() (string, error) {
	return s.GetComponentSettingString(paidsettings.PayMasterTokenKey)
}

func (s *paidSettings) GetPaidSubCryptoBotEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.CryptoBotEnabledKey)
}

func (s *paidSettings) GetPaidSubCryptoBotToken() (string, error) {
	return s.GetComponentSettingString(paidsettings.CryptoBotTokenKey)
}

func (s *paidSettings) GetPaidSubExternalEnabled() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.ExternalEnabledKey)
}

func (s *paidSettings) GetPaidSubExternalUrlTemplate() (string, error) {
	return s.GetComponentSettingString(paidsettings.ExternalURLTemplateKey)
}

func (s *paidSettings) GetPaidSubOrderTTLMinutes() (int, error) {
	return s.GetComponentSettingInt(paidsettings.OrderTTLMinutesKey)
}

func (s *paidSettings) GetPaidSubGreeting() (string, error) {
	return s.GetComponentSettingString(paidsettings.GreetingKey)
}

func (s *paidSettings) GetPaidSubRefundRevoke() (bool, error) {
	return s.GetComponentSettingBool(paidsettings.RefundRevokeKey)
}

func (s *paidSettings) GetPaidSubTransportMode() (string, error) {
	return s.GetComponentSettingString(paidsettings.TransportModeKey)
}

func (s *paidSettings) GetPaidSubOutboundTag() (string, error) {
	return s.GetComponentSettingString(paidsettings.OutboundTagKey)
}

func newPaidSubHTTPClient(runtime *service.Runtime, timeout time.Duration) (*http.Client, error) {
	settings := &paidSettings{}
	mode, err := settings.GetPaidSubTransportMode()
	if err != nil {
		return nil, fmt.Errorf("read paid subscription transport mode: %w", err)
	}
	if mode == "outbound" {
		tag, err := settings.GetPaidSubOutboundTag()
		if err != nil {
			return nil, fmt.Errorf("read paid subscription outbound tag: %w", err)
		}
		if runtime == nil {
			return nil, fmt.Errorf("paid subscription runtime is unavailable")
		}
		return service.NewOutboundHTTPClientForRuntime(runtime, tag, timeout)
	}
	if mode != "proxy" {
		return nil, fmt.Errorf("unsupported paid subscription transport mode")
	}
	cfg, err := settings.paidSubProxyConfig()
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

func (s *paidSettings) paidSubProxyConfig() (integrationtelegram.ProxyConfig, error) {
	proxyURL, err := s.GetComponentSettingString(paidsettings.ProxyURLKey)
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	username, err := s.GetComponentSettingString(paidsettings.ProxyUsernameKey)
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	password, err := s.GetComponentSettingString(paidsettings.ProxyPasswordKey)
	if err != nil {
		return integrationtelegram.ProxyConfig{}, err
	}
	return integrationtelegram.ProxyConfig{URL: proxyURL, Username: username, Password: password}, nil
}
