package service

func (s *TelegramService) TestTelegram() TelegramResult {
	return TelegramResult(s.implementation().TestTelegram())
}

func (s *TelegramService) SendTelegramDocument(filename string, data []byte, caption string) TelegramResult {
	return TelegramResult(s.implementation().SendDocument(filename, data, caption))
}

func (s *TelegramService) send(text string) TelegramResult {
	return TelegramResult(s.implementation().Send(text))
}
