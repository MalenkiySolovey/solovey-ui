package service

import "net/http"

func (s *TelegramService) getTelegramHTTPClient() (*http.Client, error) {
	return s.implementation().HTTPClient()
}
