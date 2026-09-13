//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/gin-gonic/gin"
)

func TestTrustedSourceCreateNormalizesRejectsDuplicatesAndExpiredInput(t *testing.T) {
	router, _, _, _, _ := newProtectionAPIRouterWithDB(t, writeScope, nil)
	created := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/allowlist/ips", `{"ipCidr":"198.51.100.7","reason":"administration network"}`)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"ipCidr":"198.51.100.7/32"`) {
		t.Fatalf("normalized create=%d %s", created.Code, created.Body.String())
	}
	duplicate := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/allowlist/ips", `{"ipCidr":"198.51.100.7/32","reason":"duplicate"}`)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "duplicate_trusted_source") {
		t.Fatalf("duplicate=%d %s", duplicate.Code, duplicate.Body.String())
	}
	expired := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/allowlist/ips", `{"ipCidr":"2001:db8::7","reason":"expired","expiresAt":1}`)
	if expired.Code != http.StatusBadRequest || !strings.Contains(expired.Body.String(), "validation_error") {
		t.Fatalf("expired=%d %s", expired.Code, expired.Body.String())
	}
	for _, source := range []string{"0.0.0.0/0", "127.0.0.1", "ff02::1"} {
		response := requestProtectionAPI(router, http.MethodPost, "/api/components/server-protection/allowlist/ips", `{"ipCidr":"`+source+`","reason":"unsafe"}`)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "validation_error") {
			t.Fatalf("source=%s response=%d %s", source, response.Code, response.Body.String())
		}
	}
}

func TestBroadTrustedSourceRequiresTypedConfirmation(t *testing.T) {
	router, _, _, repository, _ := newProtectionAPIRouterWithDB(t, writeScope, nil)
	path := "/api/components/server-protection/allowlist/ips"
	unconfirmed := requestProtectionAPI(router, http.MethodPost, path, `{"ipCidr":"10.0.0.0/8","reason":"private estate","broadScopeAcknowledged":true}`)
	if unconfirmed.Code != http.StatusBadRequest || !strings.Contains(unconfirmed.Body.String(), "confirmation_required") {
		t.Fatalf("unconfirmed=%d %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	confirmed := requestProtectionAPI(router, http.MethodPost, path, `{"ipCidr":"10.0.0.0/8","reason":"private estate","broadScopeAcknowledged":true,"confirmation":"TRUST BROAD SOURCE 10.0.0.0/8"}`)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirmed=%d %s", confirmed.Code, confirmed.Body.String())
	}
	items, total, err := repository.ListIPAllowlist(t.Context(), pageFixture())
	if err != nil || total != 1 || len(items) != 1 || !items[0].BroadScopeAcknowledged {
		t.Fatalf("persisted broad acknowledgement items=%#v total=%d err=%v", items, total, err)
	}
}

func TestTrustedSourceProposalPreservesClientIdentityAuthority(t *testing.T) {
	directRequest := httptest.NewRequest(http.MethodGet, "https://panel.example/api", nil)
	directRequest.RemoteAddr = "203.0.113.7:443"
	direct := clientidentity.Resolve(directRequest, clientidentity.ParseConfig(""))
	assertTrustedSourceProposal(t, direct, true, "DIRECT", "203.0.113.7/32", "")

	config := clientidentity.ParseConfig("10.0.0.0/8")
	trustedRequest := httptest.NewRequest(http.MethodGet, "https://panel.example/api", nil)
	trustedRequest.RemoteAddr = "10.0.0.2:443"
	trustedRequest.Header.Set("X-Forwarded-For", "198.51.100.7")
	trustedRequest.Header.Set("X-Forwarded-Proto", "https")
	trusted := clientidentity.Resolve(trustedRequest, config)
	assertTrustedSourceProposal(t, trusted, true, "TRUSTED_XFF", "198.51.100.7/32", "")

	unknownRequest := httptest.NewRequest(http.MethodGet, "https://panel.example/api", nil)
	unknownRequest.RemoteAddr = "10.0.0.2:443"
	unknown := clientidentity.Resolve(unknownRequest, config)
	assertTrustedSourceProposal(t, unknown, false, "", "", "client_identity_untrusted")
}

func assertTrustedSourceProposal(t *testing.T, identity clientidentity.V1, available bool, provenance, prefix, reason string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		RequireScope:   func(*gin.Context, string, ...string) bool { return true },
		ClientIdentity: func(*gin.Context) clientidentity.V1 { return identity },
		JSONObj: func(c *gin.Context, value interface{}, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": value})
		},
	})
	response := requestProtectionAPI(router, http.MethodGet, "/api/components/server-protection/allowlist/ips/proposal", "")
	var envelope struct {
		Obj trustedSourceProposalView `json:"obj"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || envelope.Obj.Available != available || envelope.Obj.Provenance != provenance || envelope.Obj.IPCIDR != prefix || envelope.Obj.ReasonCode != reason {
		t.Fatalf("proposal=%d %#v identity=%#v", response.Code, envelope.Obj, identity)
	}
	if available && (envelope.Obj.BindingRevision != clientidentity.BindingRevision(identity) || envelope.Obj.ConfigRevision != identity.ConfigRevision) {
		t.Fatalf("proposal lost identity binding: %#v", envelope.Obj)
	}
}

func pageFixture() protectionrepository.PageQuery {
	return protectionrepository.PageQuery{Page: 1, Limit: 50}
}
