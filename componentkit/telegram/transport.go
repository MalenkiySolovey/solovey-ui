//go:build !minimal

package telegram

import (
	"encoding/json"
	canonical "github.com/MalenkiySolovey/solovey-host/componentkit/telegram"
	"net/http"
)

var ErrRequest = canonical.ErrRequest
var ErrNetwork = canonical.ErrNetwork

type APIError = canonical.APIError
type APIResponse = canonical.APIResponse
type BotClient = canonical.BotClient

func NewBotClient(token string, client *http.Client) *BotClient {
	return canonical.NewBotClient(token, client)
}
func ParseResponse(method string, data []byte) (json.RawMessage, error) {
	return canonical.ParseResponse(method, data)
}
