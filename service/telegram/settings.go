package telegram

import integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"

type telegramBotCredentials struct {
	Token  string
	ChatID string
}

func (s *Service) telegramEnabled() (bool, error) {
	return s.Settings.GetTelegramEnabled()
}

func (s *Service) telegramBotCredentials() (telegramBotCredentials, Result) {
	enabled, err := s.telegramEnabled()
	if err != nil {
		return telegramBotCredentials{}, Result{ErrorClass: "settings"}
	}
	if !enabled {
		return telegramBotCredentials{}, Result{ErrorClass: "disabled"}
	}
	token, err := s.Settings.GetTelegramBotToken()
	if err != nil || token == "" {
		return telegramBotCredentials{}, Result{ErrorClass: "missing_token"}
	}
	chatID, err := s.Settings.GetTelegramChatID()
	if err != nil || chatID == "" {
		return telegramBotCredentials{}, Result{ErrorClass: "missing_chat"}
	}
	return telegramBotCredentials{Token: token, ChatID: chatID}, Result{Success: true}
}

func (s *Service) telegramProxyConfig() (integrationtelegram.ProxyConfig, error) {
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
