package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	identity "github.com/MalenkiySolovey/solovey-ui/internal/networkidentity"
	"github.com/MalenkiySolovey/solovey-ui/service/resourceinventory"
)

type networkIdentityReaderFunc func(context.Context, hostresources.ResourceSnapshot) identity.Snapshot

func (f networkIdentityReaderFunc) LocalHostNetworkIdentity(ctx context.Context, resources hostresources.ResourceSnapshot) identity.Snapshot {
	return f(ctx, resources)
}

func TestNetworkIdentityRequiresAuthenticationAndIgnoresBrowserEndpoint(t *testing.T) {
	router, cookies := deploymentTestRouter(t)
	reads, resourceReads := 0, 0
	provider := resourceinventory.NewHostIngressScopeProviderV1()
	h := &networkIdentityHTTP{
		reader: networkIdentityReaderFunc(func(ctx context.Context, resources hostresources.ResourceSnapshot) identity.Snapshot {
			reads++
			return provider.LocalHostNetworkIdentity(ctx, resources)
		}),
		resources: func(context.Context) hostresources.ResourceSnapshot {
			resourceReads++
			return hostresources.ResourceSnapshot{GeneratedAt: time.Now().Unix(), Resources: []hostresources.ProtectableResource{{
				ID: "core:panel:web", Kind: "panel_web", Owner: "core", Source: "settings", Protocol: "http", Listen: "192.168.1.2", Port: 443,
				Capabilities: hostresources.ProtectableResourceCapabilities{ConfigRevision: hostresources.Revision("configured-panel"), PublicHostnames: []string{"panel.configured.example"}},
			}}}
		},
	}
	router.GET("/api/v1/network-identity", h.snapshot)
	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/network-identity", nil))
	if unauthenticated.Code != http.StatusForbidden || reads != 0 || resourceReads != 0 {
		t.Fatal("unauthenticated request observed host network facts")
	}
	for _, browserHost := range []string{"browser-one.invalid", "browser-two.invalid"} {
		request := httptest.NewRequest(http.MethodGet, "http://"+browserHost+"/api/v1/network-identity", nil)
		request.Header.Set("X-Forwarded-Host", "untrusted-forwarded.invalid")
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, identity.SchemaV1) ||
			!strings.Contains(body, "panel.configured.example") || !strings.Contains(body, "192.168.1.2") ||
			strings.Contains(body, browserHost) || strings.Contains(body, "untrusted-forwarded.invalid") ||
			!strings.Contains(body, `"authority":"CONFIGURED_INPUT"`) || !strings.Contains(body, `"kind":"LOCAL"`) {
			t.Fatalf("status=%d body=%s", response.Code, body)
		}
	}
	if reads != 2 || resourceReads != 2 {
		t.Fatalf("reads=%d resourceReads=%d", reads, resourceReads)
	}
}
