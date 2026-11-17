package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBearerHTTPAdmissionHasNoDatabaseOrProductRuntimeClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{"backend/http/bearer", "backend/http/response", "backend/security", "backend/audit"} {
		allowed[modulePath+"/"+name] = true
	}
	for _, target := range []string{"github.com/gin-gonic/gin", "github.com/MalenkiySolovey/solovey-host/auditrecord", "github.com/MalenkiySolovey/solovey-host/redact"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, "./backend/http/bearer") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("bearer HTTP admission imports storage, execution or unrelated owners: %s", strings.Join(violations, ", "))
	}
}
