package service

type telegramBotCredentials struct {
	Token  string
	ChatID string
}

func (s *TelegramService) telegramEnabled() (bool, error) {
	return s.getBool(settingKeyTelegramEnabled)
}

func (s *TelegramService) telegramBotCredentials() (telegramBotCredentials, TelegramResult) {
	enabled, err := s.telegramEnabled()
	if err != nil {
		return telegramBotCredentials{}, TelegramResult{ErrorClass: "settings"}
	}
	if !enabled {
		return telegramBotCredentials{}, TelegramResult{ErrorClass: "disabled"}
	}
	token, err := s.getString(settingKeyTelegramBotToken)
	if err != nil || token == "" {
		return telegramBotCredentials{}, TelegramResult{ErrorClass: "missing_token"}
	}
	chatID, err := s.getString(settingKeyTelegramChatID)
	if err != nil || chatID == "" {
		return telegramBotCredentials{}, TelegramResult{ErrorClass: "missing_chat"}
	}
	return telegramBotCredentials{Token: token, ChatID: chatID}, TelegramResult{Success: true}
}

func (s *TelegramService) telegramProxyConfig() (telegramProxyConfig, error) {
	proxyURL, err := s.getString(settingKeyTelegramProxyURL)
	if err != nil {
		return telegramProxyConfig{}, err
	}
	username, err := s.getString(settingKeyTelegramProxyUsername)
	if err != nil {
		return telegramProxyConfig{}, err
	}
	password, err := s.getString(settingKeyTelegramProxyPassword)
	if err != nil {
		return telegramProxyConfig{}, err
	}
	return telegramProxyConfig{
		URL:      proxyURL,
		Username: username,
		Password: password,
	}, nil
}
