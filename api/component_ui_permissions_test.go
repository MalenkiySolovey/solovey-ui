package api

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestComponentUIReadProjectionUsesExistingScopePolicy(t *testing.T) {
	for _, row := range []struct {
		scope   string
		present bool
		want    []string
	}{
		{"", false, []string{"audit.read", "diagnostics.read"}},
		{"admin", true, []string{"audit.read", "diagnostics.read"}},
		{"read", true, []string{"diagnostics.read"}},
		{"observability", true, []string{"diagnostics.read"}},
		{"write", true, []string{"diagnostics.read"}},
		{"database", true, []string{}},
		{"unknown", true, []string{}},
		{"", true, []string{}},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if row.present {
			c.Set(apiTokenScopeKey, row.scope)
		}
		if got := componentUIReadPermissions(c); !reflect.DeepEqual(got, row.want) {
			t.Fatalf("scope %q/%v got %v want %v", row.scope, row.present, got, row.want)
		}
	}
}
