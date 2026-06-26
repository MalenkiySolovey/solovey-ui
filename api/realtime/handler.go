// Package realtimehttp owns the WebSocket lifecycle for realtime updates.
package realtimehttp

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	realtime "github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const (
	wsCloseAuth           = websocket.StatusCode(4401)
	MaxConnectionsPerUser = 5
	MaxConnectionsPerIP   = 20
	wsQueueSize           = 16
	Subprotocol           = "sui.realtime"

	defaultWSPingInterval = 25 * time.Second
	defaultWSPingTimeout  = 5 * time.Second
)

type Config struct {
	pingInterval time.Duration
	pingTimeout  time.Duration
}

type Option func(*Config)

type Handler struct {
	SettingService service.SettingService
	LoginUser      func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	Scope          func(*gin.Context) realtime.Scope
	Audit          func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj        func(*gin.Context, interface{}, error)
	JSONMsg        func(*gin.Context, string, error)
}

func defaultRealtimeConfig() Config {
	return Config{
		pingInterval: defaultWSPingInterval,
		pingTimeout:  defaultWSPingTimeout,
	}
}

func WithPingInterval(interval time.Duration) Option {
	return func(config *Config) {
		if interval > 0 {
			config.pingInterval = interval
		}
	}
}

func WithPingTimeout(timeout time.Duration) Option {
	return func(config *Config) {
		if timeout > 0 {
			config.pingTimeout = timeout
		}
	}
}

func (a *Handler) RealtimeWS(c *gin.Context) {
	a.realtimeWS(c, defaultRealtimeConfig())
}

func (a *Handler) RealtimeWSWithOptions(options ...Option) gin.HandlerFunc {
	config := defaultRealtimeConfig()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	return func(c *gin.Context) {
		a.realtimeWS(c, config)
	}
}

func (a *Handler) realtimeWS(c *gin.Context, config Config) {
	if !a.EnforceHandshakeRateLimit(c, "ws") {
		return
	}
	user := a.LoginUser(c)
	if !a.ValidateOrigin(c, user) {
		return
	}
	token, legacyProtocol := TokenFromRequest(c)
	if legacyProtocol {
		a.recordLegacyProtocolOnce(c, user)
	}
	tokenUser, ok := consumeWSToken(token)
	if !ok || tokenUser == "" || tokenUser != user {
		c.Status(http.StatusUnauthorized)
		return
	}
	ip := a.RemoteIP(c)
	releaseReservation, ok := realtime.Reserve(user, ip, MaxConnectionsPerUser, MaxConnectionsPerIP)
	if !ok {
		c.Status(http.StatusTooManyRequests)
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		Subprotocols: []string{Subprotocol},
	})
	if err != nil {
		releaseReservation()
		return
	}
	sendCh := make(chan realtime.Event, wsQueueSize)
	unregister := realtime.Register(&realtime.ClientHandle{
		User:   user,
		IP:     ip,
		Scope:  a.Scope(c),
		SendCh: sendCh,
		OnDrop: func(reason string) {
			code := wsCloseAuth
			if reason == "slow" {
				code = websocket.StatusPolicyViolation
			}
			_ = conn.Close(code, reason)
		},
	})
	defer func() {
		unregister()
		releaseReservation()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	wsCtx := conn.CloseRead(c.Request.Context())
	heartbeatCtx, stopHeartbeat := context.WithCancel(wsCtx)
	heartbeatDone := startWSHeartbeat(heartbeatCtx, conn, config)
	defer func() {
		stopHeartbeat()
		<-heartbeatDone
	}()

	select {
	case sendCh <- realtime.Event{Type: realtime.Topic("connected"), Ts: time.Now().Unix()}:
	default:
		_ = conn.Close(websocket.StatusPolicyViolation, "slow client")
		return
	}
	for {
		select {
		case event := <-sendCh:
			payload := event.Frame()
			if len(payload) == 0 {
				payload, _ = json.Marshal(event)
			}
			writeCtx, cancel := context.WithTimeout(wsCtx, 5*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				return
			}
		case <-wsCtx.Done():
			return
		}
	}
}

func startWSHeartbeat(ctx context.Context, conn *websocket.Conn, config Config) <-chan struct{} {
	done := make(chan struct{})
	pingInterval := config.pingInterval
	pingTimeout := config.pingTimeout
	go func() {
		defer close(done)
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
				err := conn.Ping(pingCtx)
				cancel()
				if err != nil {
					_ = conn.Close(websocket.StatusInternalError, "heartbeat")
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return done
}
