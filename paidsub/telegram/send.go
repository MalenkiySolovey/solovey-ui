package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
)

func (b *Bot) transport() *integrationtelegram.BotClient {
	return integrationtelegram.NewBotClient(b.token, b.client)
}

func (b *Bot) callJSON(ctx context.Context, method string, payload any) error {
	_, err := b.transport().CallJSON(ctx, method, payload)
	return err
}

func (b *Bot) getUpdates(ctx context.Context, offset int64, timeout int) ([]tgUpdate, error) {
	query := url.Values{}
	query.Set("timeout", strconv.Itoa(timeout))
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("allowed_updates", `["message","callback_query","pre_checkout_query"]`)
	result, err := b.transport().CallGET(ctx, "getUpdates", query)
	if err != nil {
		return nil, err
	}
	var updates []tgUpdate
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, fmt.Errorf("telegram getUpdates: malformed result")
	}
	return updates, nil
}

func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string, markup *inlineKeyboard) error {
	payload := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	return b.callJSON(ctx, "sendMessage", payload)
}

func (b *Bot) answerCallback(ctx context.Context, callbackID string, text string) error {
	payload := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		payload["text"] = text
	}
	return b.callJSON(ctx, "answerCallbackQuery", payload)
}

func (b *Bot) sendInvoice(ctx context.Context, chatID int64, inv *paidprovider.Invoice) error {
	payload := map[string]any{
		"chat_id": chatID, "title": inv.Title, "description": inv.Description,
		"payload": inv.Payload, "currency": inv.Currency, "prices": inv.Prices,
	}
	if inv.ProviderToken != "" {
		payload["provider_token"] = inv.ProviderToken
	}
	return b.callJSON(ctx, "sendInvoice", payload)
}

func (b *Bot) refundStarPayment(ctx context.Context, userID int64, chargeID string) error {
	return b.callJSON(ctx, "refundStarPayment", map[string]any{
		"user_id": userID, "telegram_payment_charge_id": chargeID,
	})
}

func (b *Bot) answerPreCheckout(ctx context.Context, queryID string, ok bool, errMsg string) error {
	payload := map[string]any{"pre_checkout_query_id": queryID, "ok": ok}
	if !ok && errMsg != "" {
		payload["error_message"] = errMsg
	}
	return b.callJSON(ctx, "answerPreCheckoutQuery", payload)
}

func (b *Bot) sendPhoto(ctx context.Context, chatID int64, png []byte, caption string) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("photo", "qr.png")
	if err != nil {
		return err
	}
	if _, err := part.Write(png); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	response, err := b.transport().Do(ctx, http.MethodPost, "sendPhoto", nil, &buf, writer.FormDataContentType())
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telegram sendPhoto: status %d", response.StatusCode)
	}
	return nil
}
