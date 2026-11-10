//go:build !minimal

package telegram

import (
	"context"
	"io"
	"mime/multipart"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram/service"
)

// Delivery methods bind the existing Panel operation/client owners to the one
// external feature implementation. No transport or notification policy lives here.
type DiscoveryResult = canonical.DiscoveryResult

func (s *Service) TestTelegram() Result { return s.feature().TestTelegram() }
func (s *Service) TestTelegramContext(ctx context.Context) Result {
	return s.feature().TestTelegramContext(ctx)
}
func (s *Service) Send(text string) Result { return s.feature().Send(text) }
func (s *Service) SendContext(ctx context.Context, text string) Result {
	return s.feature().SendContext(ctx, text)
}
func (s *Service) SendDocument(name string, data []byte, caption string) Result {
	return s.feature().SendDocument(name, data, caption)
}
func (s *Service) SendDocumentStream(ctx context.Context, name string, source io.Reader, caption string) Result {
	return s.feature().SendDocumentStream(ctx, name, source, caption)
}
func (s *Service) DetectChatContext(ctx context.Context, token string) DiscoveryResult {
	return s.feature().DetectChatContext(ctx, token)
}
func (s *Service) NotifyEvent(event string, fields map[string]string) {
	s.feature().NotifyEvent(event, fields)
}
func WriteDocumentMultipartStream(writer *multipart.Writer, chatID, name string, source io.Reader, caption string) error {
	return canonical.WriteDocumentMultipartStream(writer, chatID, name, source, caption)
}
func TransportErrorClass(err error) string { return canonical.TransportErrorClass(err) }
