package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/deposist/s-ui-x/realtime"

	"github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestIntegrationRealtimeWSIssueConnectPublishCloseReconnect(t *testing.T) {
	resetRateLimitState()
	resetRealtimeForTest()
	router := newIntegrationWSRouter(t)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	cookies := loginIntegrationWSUser(t, router, "admin")
	token := issueIntegrationWSToken(t, server, cookies)
	conn := dialIntegrationWS(t, server, cookies, token)
	connected := readIntegrationWSEvent(t, conn)
	if connected.Type != realtime.Topic("connected") {
		t.Fatalf("expected connected event, got %s", connected.Type)
	}

	realtime.Publish(realtime.TopicNotification, map[string]any{"phase": "phase3"})
	event := readIntegrationWSEvent(t, conn)
	if event.Type != realtime.TopicNotification {
		t.Fatalf("expected notification event, got %s", event.Type)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "phase3 reconnect")

	token = issueIntegrationWSToken(t, server, cookies)
	reconnected := dialIntegrationWS(t, server, cookies, token)
	t.Cleanup(func() { reconnected.CloseNow() })
	if event := readIntegrationWSEvent(t, reconnected); event.Type != realtime.Topic("connected") {
		t.Fatalf("expected connected event after reconnect, got %s", event.Type)
	}
}

func TestIntegrationRealtimeWSMultipleClientsReceivePublish(t *testing.T) {
	resetRateLimitState()
	resetRealtimeForTest()
	router := newIntegrationWSRouter(t)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	cookies := loginIntegrationWSUser(t, router, "admin")

	conns := make([]*websocket.Conn, 0, 2)
	for i := 0; i < 2; i++ {
		token := issueIntegrationWSToken(t, server, cookies)
		conn := dialIntegrationWS(t, server, cookies, token)
		conns = append(conns, conn)
		t.Cleanup(func() { conn.CloseNow() })
		if event := readIntegrationWSEvent(t, conn); event.Type != realtime.Topic("connected") {
			t.Fatalf("client %d expected connected event, got %s", i, event.Type)
		}
	}

	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	for i, conn := range conns {
		if event := readIntegrationWSEvent(t, conn); event.Type != realtime.TopicConfigInvalidated {
			t.Fatalf("client %d expected config invalidated event, got %s", i, event.Type)
		}
	}
}

func TestIntegrationRealtimeWSMaxPerUserCapacity(t *testing.T) {
	resetRateLimitState()
	resetRealtimeForTest()
	router := newIntegrationWSRouter(t)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	cookies := loginIntegrationWSUser(t, router, "admin")

	conns := make([]*websocket.Conn, 0, maxWSPerUser)
	for i := 0; i < maxWSPerUser; i++ {
		token := issueIntegrationWSToken(t, server, cookies)
		conn := dialIntegrationWS(t, server, cookies, token)
		conns = append(conns, conn)
		t.Cleanup(func() { conn.CloseNow() })
		if event := readIntegrationWSEvent(t, conn); event.Type != realtime.Topic("connected") {
			t.Fatalf("client %d expected connected event, got %s", i, event.Type)
		}
	}

	token := issueIntegrationWSToken(t, server, cookies)
	overflow, resp, err := dialIntegrationWSRaw(t, server, cookies, token)
	if err == nil {
		overflow.CloseNow()
		t.Fatal("expected maxWSPerUser overflow to reject websocket")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("unexpected overflow response: resp=%v err=%v", resp, err)
	}
}

func TestIntegrationRealtimeWSMaxPerIPCapacity(t *testing.T) {
	resetRateLimitState()
	resetRealtimeForTest()
	router := newIntegrationWSRouter(t)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	conns := make([]*websocket.Conn, 0, maxWSPerIP)
	for i := 0; i < maxWSPerIP; i++ {
		cookies := loginIntegrationWSUser(t, router, fmt.Sprintf("user-%02d", i))
		token := issueIntegrationWSToken(t, server, cookies)
		conn := dialIntegrationWS(t, server, cookies, token)
		conns = append(conns, conn)
		t.Cleanup(func() { conn.CloseNow() })
		if event := readIntegrationWSEvent(t, conn); event.Type != realtime.Topic("connected") {
			t.Fatalf("client %d expected connected event, got %s", i, event.Type)
		}
	}

	overflowCookies := loginIntegrationWSUser(t, router, "user-overflow")
	token := issueIntegrationWSToken(t, server, overflowCookies)
	overflow, resp, err := dialIntegrationWSRaw(t, server, overflowCookies, token)
	if err == nil {
		overflow.CloseNow()
		t.Fatal("expected maxWSPerIP overflow to reject websocket")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("unexpected IP overflow response: resp=%v err=%v", resp, err)
	}
}

func TestIntegrationRealtimeWSSlowClientDrop_XFAILPhase3(t *testing.T) {
	t.Skip("XFAIL Phase3: требуется hook для детерминированной блокировки websocket writer / заполнения ws send queue; связано с реестром п. 32 по WS reliability")
}

func newIntegrationWSRouter(t *testing.T) *gin.Engine {
	t.Helper()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login/:user", func(c *gin.Context) {
		generation, err := settingService.GetSessionGeneration()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if err := SetLoginUser(c, c.Param("user"), 0, generation); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/realtime/ws-token", (&ApiService{}).IssueWSToken)
	router.GET("/api/realtime/ws", (&ApiService{}).RealtimeWSWithOptions(WithPingInterval(time.Second), WithPingTimeout(time.Second)))
	return router
}

func loginIntegrationWSUser(t *testing.T, router *gin.Engine, user string) []*http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login/"+url.PathEscape(user), nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("login %q returned %d", user, recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login %q did not set session cookie", user)
	}
	return cookies
}

func issueIntegrationWSToken(t *testing.T, server *httptest.Server, cookies []*http.Cookie) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/realtime/ws-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", cookieHeader(cookies))
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ws-token returned %d body=%s", resp.StatusCode, string(body))
	}
	var msg struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
		Obj     struct {
			Token string `json:"token"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success || msg.Obj.Token == "" {
		t.Fatalf("unexpected ws-token response: %#v body=%s", msg, string(body))
	}
	return msg.Obj.Token
}

func dialIntegrationWS(t *testing.T, server *httptest.Server, cookies []*http.Cookie, token string) *websocket.Conn {
	t.Helper()
	conn, resp, err := dialIntegrationWSRaw(t, server, cookies, token)
	if err != nil {
		t.Fatalf("dial websocket failed: resp=%v err=%v", resp, err)
	}
	return conn
}

func dialIntegrationWSRaw(t *testing.T, server *httptest.Server, cookies []*http.Cookie, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	header.Set("Origin", server.URL)
	header.Set("Cookie", cookieHeader(cookies))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/realtime/ws?token=" + url.QueryEscape(token)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: header,
		OnPingReceived: func(context.Context, []byte) bool {
			return true
		},
	})
	return conn, resp, err
}

func readIntegrationWSEvent(t *testing.T, conn *websocket.Conn) realtime.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, reader, err := conn.Reader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var event realtime.Event
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatal(err)
	}
	return event
}
