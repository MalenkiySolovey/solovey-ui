package service

import (
	"net/http"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	telegramimpl "github.com/MalenkiySolovey/solovey-ui/service/telegram"
)

type TelegramService struct {
	SettingService
	Runtime *Runtime
	Client  *http.Client
}

func (s *TelegramService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *TelegramService) implementation() *telegramimpl.Service {
	return &telegramimpl.Service{
		Settings: &s.SettingService,
		Runtime:  telegramRuntimeAdapter{runtime: s.runtime()},
		Client:   s.Client,
	}
}

type telegramRuntimeAdapter struct {
	runtime *Runtime
}

func (a telegramRuntimeAdapter) Notifier() *integrationtelegram.Notifier {
	return a.runtime.telegram()
}

func (a telegramRuntimeAdapter) CoreHTTPClient(tag string, timeout time.Duration) (*http.Client, error) {
	return newCoreOutboundHTTPClientForRuntime(a.runtime, tag, timeout)
}

type TelegramResult telegramimpl.Result
