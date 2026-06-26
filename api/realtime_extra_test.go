package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestConsumeWSTokenExtraDoubleSpendExpiredAndCapacity(t *testing.T) {
	resetRealtimeForTest()
	setWSTokenForTest("single-use", "admin")
	if user, ok := realtimehttp.ConsumeToken("single-use"); !ok || user != "admin" {
		t.Fatalf("first token consume failed: user=%q ok=%v", user, ok)
	}
	if _, ok := realtimehttp.ConsumeToken("single-use"); ok {
		t.Fatal("second token consume should fail")
	}

	realtimehttp.StoreToken("expired", "admin", time.Now().Add(-time.Second))
	if _, ok := realtimehttp.ConsumeToken("expired"); ok {
		t.Fatal("expired token should be rejected")
	}
	if hasWSTokenForTest("expired") {
		t.Fatal("expired matching token should be consumed and removed")
	}

	base := time.Now()
	for i := 0; i < realtimehttp.MaxTokens+1; i++ {
		token := fmt.Sprintf("capacity-%04d", i)
		realtimehttp.StoreToken(token, "admin", base.Add(time.Duration(i)*time.Second))
	}
	count := realtimehttp.TokenCount()
	oldestOK := realtimehttp.HasToken("capacity-0000")
	if count != realtimehttp.MaxTokens {
		t.Fatalf("token capacity=%d, want %d", count, realtimehttp.MaxTokens)
	}
	if oldestOK {
		t.Fatal("oldest token should be evicted at capacity")
	}
}

func TestIssueWSTokenExtraRateLimit(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	resetRealtimeForTest()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login", func(c *gin.Context) {
		generation, err := settingService.GetSessionGeneration()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if err := SetLoginUser(c, "admin", 0, generation); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/realtime/ws-token", (&ApiService{}).realtimeHandler().IssueWSToken)

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	if loginRecorder.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", loginRecorder.Code)
	}

	key := realtimehttp.HandshakeRateLimitKey("ws-token", "198.51.100.10")
	for i := 0; i < realtimehttp.HandshakeLimit; i++ {
		if err := realtimehttp.CheckHandshakeRateLimit(key); err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://panel.example/api/realtime/ws-token", nil)
	req.Host = "panel.example"
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("Origin", "http://panel.example")
	for _, c := range loginRecorder.Result().Cookies() {
		req.AddCookie(c)
	}
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success || msg.Msg == "" {
		t.Fatalf("unexpected rate-limit response: %#v", msg)
	}
	if wsTokenCountForTest() != 0 {
		t.Fatal("rate-limited request should not issue a websocket token")
	}
}

func TestConsumeWSTokenTimingRegressionAnchor(t *testing.T) {
	resetRealtimeForTest()
	t.Cleanup(resetRealtimeForTest)

	const tokenCount = 256
	const iterations = 10000
	// Windows/CGO scheduling noise can move these sub-100us measurements by
	// roughly a quarter between runs. Keep the anchor tight enough to catch a
	// reintroduced early-exit scan, but not so tight it flakes on normal jitter.
	const timingVarianceBudget = 0.35
	tokens := make([]string, tokenCount)
	type rankedToken struct {
		token  string
		digest [sha256.Size]byte
	}
	ranked := make([]rankedToken, tokenCount)
	for i := 0; i < tokenCount; i++ {
		token := fmt.Sprintf("timing-token-%03d", i)
		tokens[i] = token
		ranked[i] = rankedToken{token: token, digest: sha256.Sum256([]byte(token))}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return bytes.Compare(ranked[i].digest[:], ranked[j].digest[:]) < 0
	})

	averages := measureConsumeWSTokenAverages(t, tokens, []consumeTimingCase{
		{name: "invalid", token: "timing-token-missing", wantOK: false},
		{name: "valid-first", token: ranked[0].token, wantOK: true},
		{name: "valid-middle", token: ranked[tokenCount/2].token, wantOK: true},
		{name: "valid-last", token: ranked[tokenCount-1].token, wantOK: true},
	}, iterations)
	invalidAvg := averages[0]
	validAverages := averages[1:]
	for index, validAvg := range validAverages {
		if ratio := timingDeltaRatio(validAvg, invalidAvg); ratio > timingVarianceBudget {
			t.Fatalf("valid timing rank %d differs from invalid by %.2f: valid=%s invalid=%s", index, ratio, validAvg, invalidAvg)
		}
	}
	minValid, maxValid := validAverages[0], validAverages[0]
	for _, avg := range validAverages[1:] {
		if avg < minValid {
			minValid = avg
		}
		if avg > maxValid {
			maxValid = avg
		}
	}
	if ratio := timingDeltaRatio(maxValid, minValid); ratio > timingVarianceBudget {
		t.Fatalf("valid token position timing differs by %.2f: min=%s max=%s", ratio, minValid, maxValid)
	}
}

type consumeTimingCase struct {
	name   string
	token  string
	wantOK bool
}

func measureConsumeWSTokenAverages(t *testing.T, tokens []string, cases []consumeTimingCase, iterations int) []time.Duration {
	t.Helper()
	expiresAt := time.Now().Add(time.Hour)
	totals := make([]time.Duration, len(cases))
	for i := 0; i < iterations; i++ {
		for offset := range cases {
			index := (i + offset) % len(cases)
			tc := cases[index]
			seedWSTimingTokensForTest(tokens, expiresAt)
			start := time.Now()
			user, ok := realtimehttp.ConsumeToken(tc.token)
			totals[index] += time.Since(start)
			if ok != tc.wantOK {
				t.Fatalf("%s realtimehttp.ConsumeToken(%q) ok=%v, want %v", tc.name, tc.token, ok, tc.wantOK)
			}
			if tc.wantOK && user != "admin" {
				t.Fatalf("%s realtimehttp.ConsumeToken(%q) user=%q, want admin", tc.name, tc.token, user)
			}
		}
	}
	averages := make([]time.Duration, len(cases))
	for i, total := range totals {
		averages[i] = total / time.Duration(iterations)
	}
	return averages
}

func seedWSTimingTokensForTest(tokens []string, expiresAt time.Time) {
	realtimehttp.ResetTokens()
	for _, token := range tokens {
		realtimehttp.StoreToken(token, "admin", expiresAt)
	}
}

func timingDeltaRatio(a time.Duration, b time.Duration) float64 {
	if a < b {
		a, b = b, a
	}
	return float64(a-b) / float64(a)
}
