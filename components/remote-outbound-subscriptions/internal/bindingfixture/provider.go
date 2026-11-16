//go:build !minimal

// Package fixture supplies the immutable catalog identity to Go test binaries,
// whose build metadata omits the production module dependency identity.
package bindingfixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
)

func Provider(t testing.TB) coreprovider.Identity {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture location unavailable")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../core/provider/pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct{ Sources []coreprovider.Source }
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, source := range catalog.Sources {
		if source.ID == compiledProvider {
			identity := coreprovider.ResolveModule(source.Module, source.ProviderVersion, source.ModuleSum)
			if !coreprovider.SourceIdentityVerified(identity) {
				t.Fatal("fixture provider unverified")
			}
			return identity
		}
	}
	t.Fatal("compiled fixture provider missing")
	return coreprovider.Identity{}
}
