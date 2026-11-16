package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendCredentialAlgorithmsHavePortableClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{
		modulePath + "/backend/security/crypto":   true,
		modulePath + "/backend/security/password": true,
		modulePath + "/backend/security/totp":     true,
	}
	for _, target := range []string{"golang.org/x/crypto/argon2", "golang.org/x/crypto/bcrypt", "golang.org/x/crypto/hkdf"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{"./backend/security/crypto", "./backend/security/password", "./backend/security/totp"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Fatalf("%s imports Product runtime or unrelated owner: %s", target, strings.Join(violations, ", "))
		}
	}
}
