//go:build !minimal

package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	observation "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type compatibilityHistory struct{ reads int }

func (s *compatibilityHistory) HistoryForBucketSince(observation.ObservabilityBucket, int64) ([]observation.ObservabilitySample, error) {
	s.reads++
	return []observation.ObservabilitySample{{DateTime: 1700, CPU: 3, Memory: map[string]interface{}{"current": 4}, Network: map[string]interface{}{"recv": 5}}}, nil
}
func (s *compatibilityHistory) CoreHistoryForBucketSince(observation.ObservabilityBucket, int64) ([]observation.CoreSample, error) {
	s.reads++
	return []observation.CoreSample{{DateTime: 1700, Core: map[string]interface{}{"running": true}}}, nil
}
func (s *compatibilityHistory) MetricHistory(observation.ObservabilityMetric, observation.ObservabilityBucket, int64) ([]observation.ObservabilityMetricSample, error) {
	s.reads++
	return []observation.ObservabilityMetricSample{{DateTime: 1700, Value: 3}}, nil
}

type compatibilityAudit struct{ reads int }

func (s *compatibilityAudit) ListPageFiltered(uint64, int, string, string, int64, int64) ([]model.AuditEvent, uint64, error) {
	s.reads++
	return []model.AuditEvent{}, 9, nil
}

func TestObservationHTTPCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history, audit := &compatibilityHistory{}, &compatibilityAudit{}
	allowed, limited := true, false
	deps := Deps{ObservabilityService: history, AuditService: audit, RequireScope: func(c *gin.Context, _ string, _ ...string) bool {
		if !allowed {
			c.AbortWithStatus(403)
		}
		return allowed
	}, RequireAuditAdminScope: func(c *gin.Context) bool {
		if !allowed {
			c.AbortWithStatus(403)
		}
		return allowed
	}, JSONObj: func(c *gin.Context, obj interface{}, err error) { c.JSON(200, Envelope{Success: err == nil, Obj: obj}) }, Actor: func(*gin.Context) string { return "fixture-admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" }, CheckAuditRateLimit: func(string) error {
		if limited {
			return errors.New("rate limited")
		}
		return nil
	}, AuditRateLimitKey: func(a, b string) string { return a + ":" + b }, AuditRateLimitWindow: time.Minute}
	router := gin.New()
	RegisterRoutes(router.Group("/api"), deps)
	type response struct {
		Path   string
		Status int
		Body   string
		Retry  string
	}
	var responses []response
	request := func(path string, status int) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != status {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		responses = append(responses, response{path, w.Code, strings.TrimSpace(w.Body.String()), w.Header().Get("Retry-After")})
	}
	for _, p := range []string{"/api/observability/history", "/api/observability/history?metric=cpu&bucket=30s&since=1", "/api/observability/history?metric=ram&bucket=1m", "/api/observability/history?metric=net_in&bucket=5m", "/api/observability/history?metric=net_out&bucket=2s", "/api/observability/core-history?bucket=1m&since=0", "/api/security/audit?limit=999&cursor=0&event=auth.login&severity=warn&since=1&until=1700"} {
		request(p, 200)
	}
	before := history.reads + audit.reads
	for _, p := range []string{"/api/observability/history?bucket=bad", "/api/observability/history?metric=bad", "/api/observability/history?since=-1", "/api/observability/history?since=99999999999999999999999", "/api/observability/core-history?metric=cpu", "/api/observability/core-history?bucket=bad", "/api/security/audit?limit=0", "/api/security/audit?limit=invalid", "/api/security/audit?cursor=-1", "/api/security/audit?cursor=18446744073709551616", "/api/security/audit?severity=critical", "/api/security/audit?event=bad%20event", "/api/security/audit?event=" + strings.Repeat("a", 65), "/api/security/audit?since=-1", "/api/security/audit?until=12345678901"} {
		request(p, 400)
	}
	if history.reads+audit.reads != before {
		t.Fatal("invalid query reached owner")
	}
	allowed = false
	for _, p := range []string{"/api/observability/history?bucket=bad", "/api/observability/core-history", "/api/security/audit"} {
		request(p, 403)
	}
	if history.reads+audit.reads != before {
		t.Fatal("denied request reached protected reader")
	}
	allowed = true
	limited = true
	request("/api/security/audit", 429)
	if audit.reads != 1 {
		t.Fatal("rate limit reached audit reader")
	}
	b, _ := json.Marshal(responses)
	t.Logf("http_compatibility_sha256=%x cases=%d", sha256.Sum256(b), len(responses))
	if fmt.Sprintf("%x", sha256.Sum256(b)) != "51f9c151c08777885071ca8376371723d7918698f5a3f782445f98eb78f584b7" {
		t.Fatal("baseline HTTP responses changed")
	}
}
