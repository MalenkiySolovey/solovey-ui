//go:build !minimal

package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

func (s *Service) TestTelegram() Result {
	return s.TestTelegramContext(context.Background())
}

func (s *Service) TestTelegramContext(ctx context.Context) Result {
	return s.SendContext(ctx, "Solovey UI Telegram notification test")
}

func (s *Service) SendDocument(filename string, data []byte, caption string) Result {
	return s.SendDocumentStream(context.Background(), filename, bytes.NewReader(data), caption)
}

func (s *Service) SendDocumentStream(ctx context.Context, filename string, source io.Reader, caption string) Result {
	if s == nil || ctx == nil || source == nil {
		return Result{ErrorClass: "payload"}
	}
	credentials, result := s.telegramBotCredentials()
	if !result.Success {
		return result
	}
	client, requestOwned, err := s.httpClient()
	if err != nil {
		return Result{ErrorClass: "proxy"}
	}
	if requestOwned {
		defer client.CloseIdleConnections()
	}

	bodyReader, bodyWriter := io.Pipe()
	writer := multipart.NewWriter(bodyWriter)
	writeErr := make(chan error, 1)
	go func() {
		err := WriteDocumentMultipartStream(writer, credentials.ChatID, filename, source, caption)
		if err == nil {
			err = writer.Close()
		}
		if err != nil {
			_ = bodyWriter.CloseWithError(err)
			writeErr <- err
			return
		}
		writeErr <- bodyWriter.Close()
	}()

	transport := integrationtelegram.NewBotClient(credentials.Token, client)
	response, err := transport.Do(ctx, http.MethodPost, "sendDocument", nil, bodyReader, writer.FormDataContentType())
	if err != nil {
		_ = bodyReader.CloseWithError(err)
		<-writeErr
		return Result{ErrorClass: TransportErrorClass(err)}
	}
	if err := <-writeErr; err != nil {
		return Result{ErrorClass: "payload"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{ErrorClass: integrationtelegram.StatusErrorClass(response.StatusCode)}
	}
	return Result{Success: true}
}

func WriteDocumentMultipartStream(writer *multipart.Writer, chatID string, filename string, source io.Reader, caption string) error {
	if source == nil {
		return errors.New("document source is unavailable")
	}
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if caption = integrationtelegram.Caption(caption); caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, source)
	return err
}

func (s *Service) Send(text string) Result {
	return s.SendContext(context.Background(), text)
}

func (s *Service) SendContext(ctx context.Context, text string) Result {
	if s == nil || ctx == nil {
		return Result{ErrorClass: "request"}
	}
	credentials, result := s.telegramBotCredentials()
	if !result.Success {
		return result
	}
	client, requestOwned, err := s.httpClient()
	if err != nil {
		return Result{ErrorClass: "proxy"}
	}
	if requestOwned {
		defer client.CloseIdleConnections()
	}
	payload := map[string]string{
		"chat_id": credentials.ChatID,
		"text":    redact.String(text),
	}
	response, err := integrationtelegram.NewBotClient(credentials.Token, client).DoJSON(ctx, "sendMessage", payload)
	if err != nil {
		return Result{ErrorClass: TransportErrorClass(err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{
			ErrorClass: integrationtelegram.StatusErrorClass(response.StatusCode),
			RetryAfter: integrationtelegram.RetryAfter(response.StatusCode, response.Body),
		}
	}
	return Result{Success: true}
}

func TransportErrorClass(err error) string {
	if errors.Is(err, integrationtelegram.ErrRequest) {
		return "request"
	}
	return "network"
}
