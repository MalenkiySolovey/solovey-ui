package registry

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service"
)

func TestIsolatedContextDiscardsMutableLiveRegistry(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	live := Context(parent)
	service.MustRegister[adapter.CertificateProviderManager](live, &contextProviderManager{})
	isolated := Context(live)
	if service.FromContext[adapter.CertificateProviderManager](isolated) != nil {
		t.Fatal("validation retained live manager")
	}
	if service.FromContext[adapter.CertificateProviderRegistry](isolated) == service.FromContext[adapter.CertificateProviderRegistry](live) {
		t.Fatal("provider registries shared")
	}
	if service.FromContext[adapter.InboundRegistry](isolated) == service.FromContext[adapter.InboundRegistry](live) {
		t.Fatal("inbound registries shared")
	}
	cancel()
	if isolated.Err() != context.Canceled {
		t.Fatal("isolation dropped cancellation")
	}
}

type contextProviderManager struct {
	adapter.CertificateProviderManager
}
