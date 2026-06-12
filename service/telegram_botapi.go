package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

const telegramMaxRetryAfter = 300 * time.Second

func (s *TelegramService) TestTelegram() TelegramResult {
	return s.send("S-UI Telegram notification test")
}

func (s *TelegramService) SendTelegramDocument(filename string, data []byte, caption string) TelegramResult {
	credentials, result := s.telegramBotCredentials()
	if !result.Success {
		return result
	}

	bodyReader, bodyWriter := io.Pipe()
	writer := multipart.NewWriter(bodyWriter)
	writeErr := make(chan error, 1)
	go func() {
		err := writeTelegramDocumentMultipart(writer, credentials.ChatID, filename, data, caption)
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.telegram.org/bot"+credentials.Token+"/sendDocument", bodyReader)
	if err != nil {
		_ = bodyReader.CloseWithError(err)
		<-writeErr
		return TelegramResult{ErrorClass: "request"}
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client, err := s.getTelegramHTTPClient()
	if err != nil {
		_ = bodyReader.CloseWithError(err)
		<-writeErr
		return TelegramResult{ErrorClass: "proxy"}
	}
	resp, err := client.Do(req)
	if err != nil {
		_ = bodyReader.CloseWithError(err)
		<-writeErr
		return TelegramResult{ErrorClass: "network"}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := <-writeErr; err != nil {
		return TelegramResult{ErrorClass: "payload"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TelegramResult{ErrorClass: telegramStatusErrorClass(resp.StatusCode)}
	}
	return TelegramResult{Success: true}
}

func writeTelegramDocumentMultipart(writer *multipart.Writer, chatID string, filename string, data []byte, caption string) error {
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if caption = telegramCaption(caption); caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, bytes.NewReader(data))
	return err
}

func (s *TelegramService) send(text string) TelegramResult {
	credentials, result := s.telegramBotCredentials()
	if !result.Success {
		return result
	}
	payload, err := json.Marshal(map[string]string{
		"chat_id": credentials.ChatID,
		"text":    redact.String(text),
	})
	if err != nil {
		return TelegramResult{ErrorClass: "payload"}
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.telegram.org/bot"+credentials.Token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return TelegramResult{ErrorClass: "request"}
	}
	req.Header.Set("Content-Type", "application/json")
	client, err := s.getTelegramHTTPClient()
	if err != nil {
		return TelegramResult{ErrorClass: "proxy"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return TelegramResult{ErrorClass: "network"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return TelegramResult{
			ErrorClass: telegramStatusErrorClass(resp.StatusCode),
			RetryAfter: telegramRetryAfter(resp.StatusCode, body),
		}
	}
	return TelegramResult{Success: true}
}

func telegramRetryAfter(status int, body []byte) time.Duration {
	if status != http.StatusTooManyRequests || len(body) == 0 {
		return 0
	}
	var response struct {
		OK         bool `json:"ok"`
		ErrorCode  int  `json:"error_code"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0
	}
	if response.ErrorCode != http.StatusTooManyRequests || response.Parameters.RetryAfter <= 0 {
		return 0
	}
	retryAfter := time.Duration(response.Parameters.RetryAfter) * time.Second
	if retryAfter > telegramMaxRetryAfter {
		return telegramMaxRetryAfter
	}
	return retryAfter
}

func telegramStatusErrorClass(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusNotFound:
		return "chat_not_found"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "unknown"
	}
}

func telegramCaption(caption string) string {
	return util.SafeHeader(redact.String(caption), 1024)
}
