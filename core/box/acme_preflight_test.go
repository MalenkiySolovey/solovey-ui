//go:build with_acme

package box

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

func TestNativeACMEFullPreflightDoesNotIssue(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	for _, authority := range []string{server.URL + "/directory", "zerossl"} {
		directory := filepath.Join(t.TempDir(), "not-started")
		raw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "certificate_providers": []map[string]any{{"type": "acme", "tag": "native", "domain": []string{"fixture.invalid"}, "email": "operator@fixture.invalid", "provider": authority, "data_directory": directory}}})
		ctx := registry.Context(context.Background())
		var options option.Options
		if err := sbjson.UnmarshalContext(ctx, raw, &options); err != nil {
			t.Fatal("pinned decode rejected the offline candidate")
		}
		instance, err := NewBox(Options{Context: ctx, Options: options})
		if err != nil {
			t.Fatal("full native constructor rejected an accepted candidate")
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatal("preflight started the provider file lifecycle")
		}
	}
	if requests.Load() != 0 {
		t.Fatal("preflight contacted the fake certificate authority")
	}
}
