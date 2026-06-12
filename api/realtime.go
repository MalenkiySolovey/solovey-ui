package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/realtime"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const (
	wsCloseAuth   = websocket.StatusCode(4401)
	maxWSPerUser  = 5
	maxWSPerIP    = 20
	wsQueueSize   = 16
	wsSubprotocol = "sui.realtime"

	defaultWSPingInterval = 25 * time.Second
	defaultWSPingTimeout  = 5 * time.Second
)

type realtimeConfig struct {
	pingInterval time.Duration
	pingTimeout  time.Duration
}

type realtimeOption func(*realtimeConfig)

func defaultRealtimeConfig() realtimeConfig {
	return realtimeConfig{
		pingInterval: defaultWSPingInterval,
		pingTimeout:  defaultWSPingTimeout,
	}
}

func WithPingInterval(interval time.Duration) realtimeOption {
	return func(config *realtimeConfig) {
		if interval > 0 {
			config.pingInterval = interval
		}
	}
}

func WithPingTimeout(timeout time.Duration) realtimeOption {
	return func(config *realtimeConfig) {
		if timeout > 0 {
			config.pingTimeout = timeout
		}
	}
}

func (a *ApiService) RealtimeWS(c *gin.Context) {
	a.realtimeWS(c, defaultRealtimeConfig())
}

func (a *ApiService) RealtimeWSWithOptions(options ...realtimeOption) gin.HandlerFunc {
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

func (a *ApiService) realtimeWS(c *gin.Context, config realtimeConfig) {
	if !a.enforceWSHandshakeRateLimit(c, "ws") {
		return
	}
	user := GetLoginUser(c)
	if !a.validateWSOrigin(c, user) {
		return
	}
	token, legacyProtocol := wsTokenFromRequest(c)
	if legacyProtocol {
		a.recordLegacyWSProtocolAuditOnce(c, user)
	}
	tokenUser, ok := consumeWSToken(token)
	if !ok || tokenUser == "" || tokenUser != user {
		c.Status(http.StatusUnauthorized)
		return
	}
	ip := getRemoteIp(c)
	releaseReservation, ok := realtime.Reserve(user, ip, maxWSPerUser, maxWSPerIP)
	if !ok {
		c.Status(http.StatusTooManyRequests)
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		Subprotocols: []string{wsSubprotocol},
	})
	if err != nil {
		releaseReservation()
		return
	}
	sendCh := make(chan realtime.Event, wsQueueSize)
	unregister := realtime.Register(&realtime.ClientHandle{
		User:   user,
		IP:     ip,
		Scope:  realtimeScopeFromContext(c),
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
			payload, _ := json.Marshal(event)
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

func realtimeScopeFromContext(c *gin.Context) realtime.Scope {
	switch c.GetString(apiTokenScopeKey) {
	case "":
		return realtime.ScopeAdmin
	case string(realtime.ScopeAdmin):
		return realtime.ScopeAdmin
	case string(realtime.ScopeRead):
		return realtime.ScopeRead
	case string(realtime.ScopeWrite):
		return realtime.ScopeWrite
	case string(realtime.ScopeObservability):
		return realtime.ScopeObservability
	default:
		return realtime.ScopeRead
	}
}

func startWSHeartbeat(ctx context.Context, conn *websocket.Conn, config realtimeConfig) <-chan struct{} {
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
