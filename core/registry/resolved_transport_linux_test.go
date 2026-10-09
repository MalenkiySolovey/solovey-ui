//go:build linux

package registry

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/service/resolved"
	"github.com/sagernet/sing/service"
)

type fixtureServices struct {
	adapter.ServiceManager
	current adapter.Service
}

func (f *fixtureServices) Get(tag string) (adapter.Service, bool) {
	return f.current, tag == "resolver" && f.current != nil
}

func TestResolvedOfficialTransportBindsSameCoreService(t *testing.T) {
	manager := &fixtureServices{}
	ctx := service.ContextWith[adapter.ServiceManager](Context(context.Background()), manager)
	logger := log.NewNOPFactory().NewLogger("fixture")
	transport, err := DNSTransportRegistry().CreateDNSTransport(ctx, logger, "resolved", "resolved", &option.ResolvedDNSServerOptions{Service: "resolver"})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if _, real := transport.(*resolved.Transport); !real {
		t.Fatal("resolved registry returned prepared stub")
	}
	if err := transport.Start(adapter.StartStateInitialize); err == nil {
		t.Fatal("missing same-core service accepted")
	}
	owner, err := ServiceRegistry().Create(ctx, logger, "resolver", "resolved", &option.ResolvedServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Construction/binding only: no bus, listener, host service or name mutation.
	manager.current = owner
	if err := transport.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	if _, real := owner.(*resolved.Service); !real {
		t.Fatal("resolved service constructor is not official")
	}
	manager.current = nil
	if err := transport.Start(adapter.StartStateInitialize); err == nil {
		t.Fatal("removed service accepted on fresh initialization")
	}
}
