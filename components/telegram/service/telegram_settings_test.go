//go:build !minimal

package telegram_test

import telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"

type testTelegramSettings struct {
	telegramsettings.Reader
}
