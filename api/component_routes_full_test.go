//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
)

func prepareComponentRouteMetadata(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	components := registry.Components()
	items := make([]installstate.InstalledComponent, 0, len(components))
	for _, component := range components {
		items = append(items, installstate.InstalledComponent{
			ID:        component.Manifest.ID,
			Delivery:  component.Manifest.Delivery,
			Installed: true,
		})
	}
	data, err := json.Marshal(installstate.Metadata{
		Version:    1,
		Profile:    "full",
		Binary:     "full",
		Components: items,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := installstate.Store(path, mustUnmarshalInstalledMetadata(t, data)); err != nil {
		t.Fatal(err)
	}
}

func mustUnmarshalInstalledMetadata(t *testing.T, data []byte) installstate.Metadata {
	t.Helper()
	var metadata installstate.Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	return metadata
}

func expectedOptionalAPIPostRoutes() []string {
	return []string{
		"/api/import-xui",
		"/api/import-xui/plan",
		"/api/import-xui/apply",
		"/api/import-xui/rollback",
		"/api/paidsub/bindings",
		"/api/paidsub/tariffs",
		"/api/paidsub/refund",
		"/api/paidsub/broadcast",
		"/api/remote-outbound-subscriptions/save",
		"/api/remote-outbound-subscriptions/delete",
		"/api/remote-outbound-subscriptions/refresh",
		"/api/remote-outbound-subscriptions/groups/save",
		"/api/remote-outbound-subscriptions/groups/bulk",
		"/api/remote-outbound-subscriptions/groups/delete",
		"/api/remote-outbound-subscriptions/groups/connections",
		"/api/remote-outbound-subscriptions/groups/outbounds",
		"/api/remote-outbound-subscriptions/connections/group",
		"/api/remote-outbound-subscriptions/connections/sync",
		"/api/telegram/test",
		"/api/telegram/backup",
		"/api/telegram/backup/run",
		"/api/update/check",
		"/api/update/apply",
		"/api/update/components/:id/enable",
		"/api/update/components/:id/disable",
		"/api/update/components/:id/install",
		"/api/update/components/:id/remove",
	}
}

func securityAuthZOptionalRows() []securityAuthZRow {
	return []securityAuthZRow{
		{method: http.MethodGet, path: "/apiv2/security/audit", auditAdmin: true},
		{method: http.MethodPost, path: "/apiv2/import-xui", resource: "database", allowed: []string{"admin", "database"}},
		{method: http.MethodPost, path: "/apiv2/import-xui/plan", resource: "database", allowed: []string{"admin", "database"}},
		{method: http.MethodPost, path: "/apiv2/import-xui/apply", resource: "database", allowed: []string{"admin", "database"}},
		{method: http.MethodPost, path: "/apiv2/import-xui/rollback", resource: "database", allowed: []string{"admin", "database"}},
		{method: http.MethodGet, path: "/apiv2/import-xui/reports", resource: "database", allowed: []string{"admin", "database"}},
		{method: http.MethodPost, path: "/apiv2/telegram/test", resource: "telegram", allowed: []string{"admin"}},
		{method: http.MethodPost, path: "/apiv2/telegram/backup", resource: "telegram", allowed: []string{"telegram", "admin"}},
		{method: http.MethodPost, path: "/apiv2/telegram/backup/run", resource: "telegram", allowed: []string{"telegram", "admin"}},
		{method: http.MethodGet, path: "/api/remote-outbound-subscriptions", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "read", "write"}},
		{method: http.MethodGet, path: "/api/remote-outbound-subscriptions/collected", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "read", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/save", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/delete", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/refresh", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodGet, path: "/api/remote-outbound-subscriptions/test", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodGet, path: "/api/remote-outbound-subscriptions/test-all", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/groups/save", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/groups/bulk", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/groups/delete", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/groups/connections", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/groups/outbounds", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/connections/group", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/remote-outbound-subscriptions/connections/sync", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodGet, path: "/api/remote-outbound-subscriptions/connections/test", resource: "remoteOutboundSubscriptions", allowed: []string{"admin", "write"}},
		{method: http.MethodPost, path: "/api/update/components/telegram/enable", resource: "update", allowed: []string{"admin", "update"}},
		{method: http.MethodPost, path: "/api/update/components/telegram/disable", resource: "update", allowed: []string{"admin", "update"}},
		{method: http.MethodPost, path: "/api/update/components/telegram/install", resource: "update", allowed: []string{"admin", "update"}},
		{method: http.MethodPost, path: "/api/update/components/telegram/remove", resource: "update", allowed: []string{"admin", "update"}},
	}
}

func expectedOptionalAPIGetRoutes() []string {
	return []string{
		"/api/import-xui/reports",
		"/api/observability/history",
		"/api/observability/core-history",
		"/api/paidsub/bindings",
		"/api/paidsub/tariffs",
		"/api/paidsub/orders",
		"/api/paidsub/status",
		"/api/remote-outbound-subscriptions",
		"/api/remote-outbound-subscriptions/collected",
		"/api/remote-outbound-subscriptions/test",
		"/api/remote-outbound-subscriptions/test-all",
		"/api/remote-outbound-subscriptions/connections/test",
		"/api/security/audit",
		"/api/update/status",
		"/api/update/components",
	}
}

func securityCSRFOptionalPostRoutes() []string {
	return []string{
		"/api/components/server-protection/settings",
		"/api/components/server-protection/profiles",
		"/api/components/server-protection/profiles/1",
		"/api/components/server-protection/profiles/1/reattach",
		"/api/components/server-protection/events",
		"/api/components/server-protection/graylist",
		"/api/components/server-protection/allowlist/ports",
		"/api/components/server-protection/allowlist/ports/1",
		"/api/components/server-protection/allowlist/ips",
		"/api/components/server-protection/allowlist/ips/1",
		"/api/components/server-protection/firewall/preview",
		"/api/components/server-protection/fronting/preview",
		"/api/components/server-protection/fronting/sync",
		"/api/components/server-protection/fronting/apply",
		"/api/components/server-protection/fronting/rollback",
		"/api/components/server-protection/firewall/prepare",
		"/api/components/server-protection/firewall/apply",
		"/api/components/server-protection/firewall/rollback",
		"/api/components/server-protection/operations/operation-1/force-unlock",
		"/api/components/server-protection/operations/operation-1/forget-state",
		"/api/components/server-protection/ports/prepare",
		"/api/components/server-protection/ports/apply",
		"/api/components/server-protection/ports/rollback",
		"/api/components/server-protection/native-fallback/preview",
		"/api/components/server-protection/native-fallback/prepare",
		"/api/components/server-protection/native-fallback/apply",
		"/api/components/server-protection/native-fallback/rollback",
		"/api/import-xui",
		"/api/import-xui/plan",
		"/api/import-xui/apply",
		"/api/import-xui/rollback",
		"/api/paidsub/bindings",
		"/api/paidsub/tariffs",
		"/api/paidsub/broadcast",
		"/api/paidsub/refund",
		"/api/remote-outbound-subscriptions/save",
		"/api/remote-outbound-subscriptions/delete",
		"/api/remote-outbound-subscriptions/refresh",
		"/api/remote-outbound-subscriptions/groups/save",
		"/api/remote-outbound-subscriptions/groups/bulk",
		"/api/remote-outbound-subscriptions/groups/delete",
		"/api/remote-outbound-subscriptions/groups/connections",
		"/api/remote-outbound-subscriptions/groups/outbounds",
		"/api/remote-outbound-subscriptions/connections/group",
		"/api/remote-outbound-subscriptions/connections/sync",
		"/api/telegram/test",
		"/api/telegram/backup",
		"/api/telegram/backup/run",
		"/api/update/check",
		"/api/update/apply",
		"/api/update/components/telegram/enable",
		"/api/update/components/telegram/disable",
		"/api/update/components/telegram/install",
		"/api/update/components/telegram/remove",
	}
}
