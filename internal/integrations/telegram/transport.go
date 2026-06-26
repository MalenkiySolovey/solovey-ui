package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const botAPIBase = "https://api.telegram.org"
const maxTelegramResponseBytes = 1 << 20

var (
	ErrRequest = errors.New("telegram request error")
	ErrNetwork = errors.New("telegram network error")
)

type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s failed: code=%d %s", e.Method, e.Code, e.Description)
}

type APIResponse struct {
	StatusCode int
	Body       []byte
}

type BotClient struct {
	token string
	http  *http.Client
}

func NewBotClient(token string, client *http.Client) *BotClient {
	return &BotClient{token: token, http: client}
}

func (c *BotClient) DoJSON(ctx context.Context, method string, payload any) (APIResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return APIResponse{}, err
	}
	return c.Do(ctx, http.MethodPost, method, nil, bytes.NewReader(body), "application/json")
}

func (c *BotClient) CallJSON(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	response, err := c.DoJSON(ctx, method, payload)
	if err != nil {
		return nil, err
	}
	return ParseResponse(method, response.Body)
}

func (c *BotClient) CallGET(ctx context.Context, method string, query url.Values) (json.RawMessage, error) {
	response, err := c.Do(ctx, http.MethodGet, method, query, nil, "")
	if err != nil {
		return nil, err
	}
	return ParseResponse(method, response.Body)
}

func (c *BotClient) Do(ctx context.Context, httpMethod, method string, query url.Values, body io.Reader, contentType string) (APIResponse, error) {
	if c == nil || c.http == nil {
		return APIResponse{}, ErrNetwork
	}
	u := botAPIBase + "/bot" + c.token + "/" + method
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, httpMethod, u, body)
	if err != nil {
		return APIResponse{}, ErrRequest
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return APIResponse{}, ErrNetwork
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxTelegramResponseBytes))
	return APIResponse{StatusCode: resp.StatusCode, Body: data}, nil
}

func ParseResponse(method string, data []byte) (json.RawMessage, error) {
	var response struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  *struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("telegram %s: malformed response", method)
	}
	if !response.OK {
		apiErr := &APIError{Method: method, Code: response.ErrorCode, Description: response.Description}
		if response.Parameters != nil {
			apiErr.RetryAfter = response.Parameters.RetryAfter
		}
		return nil, apiErr
	}
	return response.Result, nil
}
