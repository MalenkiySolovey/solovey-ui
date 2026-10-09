package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeGroupProbeResolvesMembershipAndPublishesOfficialHistory(t *testing.T) {
	core := startPrivateFixture(t)
	generation := core.RuntimeStatus(t.Context()).Generation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	for _, group := range []string{"missing", "direct"} {
		if result := core.CheckRuntimeGroupOutbound(t.Context(), generation, group, "direct", server.URL); result.OK || result.Error != "member_not_in_group" {
			t.Fatal("unproven group target admitted", result)
		}
	}
	if result := core.CheckRuntimeGroupOutbound(t.Context(), "00000000-0000-4000-8000-000000000001", "choice", "direct", server.URL); result.Error != "stale_generation" {
		t.Fatal("stale probe admitted", result)
	}
	if result := core.CheckRuntimeGroupOutbound(t.Context(), generation, "choice", "direct", server.URL); !result.OK {
		t.Fatal("local member probe failed", result)
	}
	groups, err := core.Groups(t.Context(), generation)
	if err != nil || groups.Groups[0].Items[0].TestedAt == 0 {
		t.Fatal("manual probe not reflected by official group history", err)
	}
}
