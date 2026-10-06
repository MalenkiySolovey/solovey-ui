//go:build !minimal

package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

const discoveryTimeout = 10 * time.Second
const discoveryUpdateLimit = 100

type DiscoveryResult struct {
	Success    bool   `json:"success"`
	ChatID     string `json:"chatId,omitempty"`
	ErrorClass string `json:"errorClass,omitempty"`
}

// DetectChatContext performs one explicit, bounded discovery operation. A
// supplied token is request-local; the Settings owner alone persists secrets.
func (s *Service) DetectChatContext(ctx context.Context, suppliedToken string) DiscoveryResult {
	if s == nil || ctx == nil {
		return DiscoveryResult{ErrorClass: "request"}
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	if class := discoveryContextError(ctx); class != "" {
		return DiscoveryResult{ErrorClass: class}
	}
	leaseCtx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		if class := discoveryContextError(ctx); class != "" {
			return DiscoveryResult{ErrorClass: class}
		}
		return DiscoveryResult{ErrorClass: "maintenance"}
	}
	defer release()
	ctx = leaseCtx
	if s.Settings == nil {
		return DiscoveryResult{ErrorClass: "settings"}
	}
	token := strings.TrimSpace(suppliedToken)
	if token == "" {
		token, err = s.Settings.GetTelegramBotToken()
		if err != nil {
			return DiscoveryResult{ErrorClass: "settings"}
		}
	}
	if token == "" {
		return DiscoveryResult{ErrorClass: "missing_token"}
	}
	client, owned, err := s.httpClient()
	if err != nil || client == nil {
		return DiscoveryResult{ErrorClass: "proxy"}
	}
	if owned {
		defer client.CloseIdleConnections()
	}
	// Do not mutate shared/injected clients or forward the token via redirects.
	localClient := *client
	localClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := integrationtelegram.NewBotClient(token, &localClient).Do(ctx, http.MethodGet, "getUpdates", url.Values{"limit": {"100"}, "timeout": {"0"}}, nil, "")
	if err != nil {
		if class := discoveryContextError(ctx); class != "" {
			return DiscoveryResult{ErrorClass: class}
		}
		if errors.Is(err, integrationtelegram.ErrRequest) {
			return DiscoveryResult{ErrorClass: "request"}
		}
		return DiscoveryResult{ErrorClass: "network"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DiscoveryResult{ErrorClass: discoveryProviderError(response.StatusCode)}
	}
	return discoveryChat(response.Body)
}

func discoveryContextError(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	return ""
}

func discoveryProviderError(code int) string {
	switch code {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "telegram_error"
	}
}

type discoveryChatValue struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type discoveryMessage struct {
	Chat *discoveryChatValue `json:"chat"`
}
type discoveryUpdate struct {
	ID                *int64            `json:"update_id"`
	Message           *discoveryMessage `json:"message"`
	EditedMessage     *discoveryMessage `json:"edited_message"`
	ChannelPost       *discoveryMessage `json:"channel_post"`
	EditedChannelPost *discoveryMessage `json:"edited_channel_post"`
	MyChatMember      *discoveryMessage `json:"my_chat_member"`
}

func (u discoveryUpdate) usableChat() string {
	for _, message := range []*discoveryMessage{u.Message, u.EditedMessage, u.ChannelPost, u.EditedChannelPost, u.MyChatMember} {
		if message == nil || message.Chat == nil || message.Chat.ID == 0 {
			continue
		}
		switch message.Chat.Type {
		case "private", "group", "supergroup", "channel":
			return strconv.FormatInt(message.Chat.ID, 10)
		}
	}
	return ""
}

func discoveryChat(body []byte) DiscoveryResult {
	var envelope struct {
		OK        *bool           `json:"ok"`
		Result    json.RawMessage `json:"result"`
		ErrorCode int             `json:"error_code"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.OK == nil {
		return DiscoveryResult{ErrorClass: "response"}
	}
	if !*envelope.OK {
		return DiscoveryResult{ErrorClass: discoveryProviderError(envelope.ErrorCode)}
	}
	decoder := json.NewDecoder(bytes.NewReader(envelope.Result))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('[') {
		return DiscoveryResult{ErrorClass: "response"}
	}
	var selected string
	var latest int64 = -1
	for count := 0; decoder.More(); count++ {
		if count >= discoveryUpdateLimit {
			return DiscoveryResult{ErrorClass: "response"}
		}
		var update discoveryUpdate
		if decoder.Decode(&update) != nil {
			return DiscoveryResult{ErrorClass: "response"}
		}
		if update.ID == nil || *update.ID < 0 || *update.ID < latest {
			continue
		}
		if chatID := update.usableChat(); chatID != "" {
			selected = chatID
			latest = *update.ID
		}
	}
	if last, err := decoder.Token(); err != nil || last != json.Delim(']') {
		return DiscoveryResult{ErrorClass: "response"}
	}
	if selected == "" {
		return DiscoveryResult{ErrorClass: "no_chat"}
	}
	return DiscoveryResult{Success: true, ChatID: selected}
}
