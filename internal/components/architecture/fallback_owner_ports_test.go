package architecture

import (
	"path/filepath"
	"testing"
)

func TestFallbackTargetConsumersCannotReadPrivateHostOwners(t *testing.T) {
	for _, relative := range []string{
		"components/fallback-html/service/targets.go",
		"components/fallback-html/service/health.go",
		"components/fallback-html/service/service.go",
		"components/fallback-html/service/host.go",
		"components/fallback-html/service/target_observation.go",
		"components/fallback-html/service/runtime.go",
		"components/fallback-html/service/http_host.go",
	} {
		for _, imported := range fileImports(t, filepath.Join(moduleRoot(t), filepath.FromSlash(relative))) {
			if imported == modulePath+"/service" || imported == modulePath+"/database/sqlite" || imported == modulePath+"/network/bind" || imported == modulePath+"/middleware/security" || imported == "crypto/tls" {
				t.Errorf("%s bypasses injected host facts: %s", relative, imported)
			}
		}
	}
	for _, relative := range []string{"components/fallback-html/service/host.go", "components/fallback-html/service/target_observation.go", "components/fallback-html/service/http_host.go"} {
		for _, imported := range fileImports(t, filepath.Join(moduleRoot(t), filepath.FromSlash(relative))) {
			if imported == "gorm.io/gorm" || imported == "database/sql" || imported == "os/exec" || imported == "crypto/tls" {
				t.Errorf("%s exposes a concrete host execution capability: %s", relative, imported)
			}
		}
	}
}
