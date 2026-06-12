package service

import "time"

type TelegramService struct {
	SettingService
	Runtime *Runtime
}

func (s *TelegramService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

type TelegramResult struct {
	Success    bool          `json:"success"`
	ErrorClass string        `json:"errorClass,omitempty"`
	RetryAfter time.Duration `json:"-"`
}
