package web

import (
	"regexp"
	"testing"
)

func TestProviderMaintenanceCompressionExclusionIsExact(t *testing.T) {
	pattern := regexp.MustCompile(providerMaintenanceUncompressedPattern("/qualification+v1/"))
	if !pattern.MatchString("/qualification+v1/api/v1/operations/update/provider/maintain") {
		t.Fatal("provider maintenance lost native deadline control")
	}
	for _, path := range []string{
		"/qualificationv1/api/v1/operations/update/provider/maintain",
		"/qualification+v1/api/v1/operations/update/provider/maintain/extra",
		"/qualification+v1/api/v1/operations/update/provider/preview",
		"/qualification+v1/apiv2/v1/operations/update/provider/maintain",
		"/qualification+v1/api/clients",
	} {
		if pattern.MatchString(path) {
			t.Fatal("unrelated route lost compression", path)
		}
	}
}
