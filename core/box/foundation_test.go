package box

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/sagernet/sing-box/adapter"
	certificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

func TestBoxFreshRequiredContext(t *testing.T) {
	var previous *Box
	for range 2 {
		instance, err := NewBox(Options{Context: registry.Context(context.Background()), Options: option.Options{Log: &option.LogOptions{Disabled: true}}})
		if err != nil {
			t.Fatal(err)
		}
		if service.FromContext[log.Factory](instance.Context()) != instance.LogFactory() ||
			service.FromContext[adapter.CertificateProviderManager](instance.Context()) == nil ||
			service.FromContext[adapter.HTTPClientManager](instance.Context()) == nil ||
			service.FromContext[adapter.NetworkNamespaceManager](instance.Context()) == nil ||
			service.PtrFromContext[trafficcontrol.Manager](instance.Context()) != instance.traffic {
			t.Fatal("incomplete generation context")
		}
		if previous != nil && (instance.traffic == previous.traffic || instance.httpClient == previous.httpClient || instance.certificateProvider == previous.certificateProvider) {
			t.Fatal("shared mutable generation owner")
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
		previous = instance
	}
}

func TestBoxConstructorFailureClosesUnstartedProviderOnce(t *testing.T) {
	ctx := registry.Context(context.Background())
	provider := &foundationProvider{tag: "first"}
	witness := errors.New("constructor witness")
	certificate.Register[option.StubOptions](service.FromContext[adapter.CertificateProviderRegistry](ctx).(*certificate.Registry), "test", func(_ context.Context, _ log.ContextLogger, tag string, _ option.StubOptions) (adapter.CertificateProviderService, error) {
		if tag == "second" {
			return nil, witness
		}
		return provider, nil
	})
	_, err := NewBox(Options{Context: ctx, Options: option.Options{Log: &option.LogOptions{Disabled: true}, CertificateProviders: []option.CertificateProvider{{Type: "test", Tag: "first"}, {Type: "test", Tag: "second"}}}})
	if err == nil || !strings.Contains(err.Error(), witness.Error()) {
		t.Fatalf("constructor error=%v", err)
	}
	if provider.closes != 1 || len(provider.stages) != 0 {
		t.Fatalf("constructor cleanup=%+v", provider)
	}
}

func TestBoxProviderLifecycleOrderAndFailureRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			ctx := registry.Context(context.Background())
			events := []string{}
			provider := &foundationProvider{tag: "provider", events: &events, fail: fail}
			listener := &foundationInbound{events: &events}
			certificate.Register[option.StubOptions](service.FromContext[adapter.CertificateProviderRegistry](ctx).(*certificate.Registry), "test", func(context.Context, log.ContextLogger, string, option.StubOptions) (adapter.CertificateProviderService, error) {
				return provider, nil
			})
			inbound.Register[option.StubOptions](service.FromContext[adapter.InboundRegistry](ctx).(*inbound.Registry), "test", func(context.Context, adapter.Router, log.ContextLogger, string, option.StubOptions) (adapter.Inbound, error) {
				return listener, nil
			})
			instance, err := NewBox(Options{Context: ctx, Options: option.Options{Log: &option.LogOptions{Disabled: true}, CertificateProviders: []option.CertificateProvider{{Type: "test", Tag: "provider"}}, Inbounds: []option.Inbound{{Type: "test", Tag: "listener"}}}})
			if err != nil {
				t.Fatal(err)
			}
			err = instance.Start()
			if fail && err == nil || !fail && err != nil {
				t.Fatalf("Start error=%v fail=%v", err, fail)
			}
			if err := instance.Close(); err != nil {
				t.Fatal(err)
			}
			_ = instance.Close()
			if provider.closes != 1 || listener.closes != 1 {
				t.Fatalf("cleanup provider=%d inbound=%d", provider.closes, listener.closes)
			}
			if fail {
				if !reflect.DeepEqual(listener.stages, []adapter.StartStage{adapter.StartStateInitialize}) {
					t.Fatalf("inbound started despite provider failure: %v", listener.stages)
				}
			} else {
				index := func(value string) int {
					for i, event := range events {
						if event == value {
							return i
						}
					}
					return -1
				}
				if index("provider/start") >= index("inbound/start") {
					t.Fatalf("provider/inbound startup order: %v", events)
				}
			}
		})
	}
}

func TestBoxRejectsDuplicateProviderBeforeConstruction(t *testing.T) {
	ctx := registry.Context(context.Background())
	_, err := NewBox(Options{Context: ctx, Options: option.Options{CertificateProviders: []option.CertificateProvider{{Type: "acme", Tag: "duplicate"}, {Type: "acme", Tag: "duplicate"}}}})
	if err == nil {
		t.Fatal("duplicate provider accepted")
	}
}

type foundationProvider struct {
	tag    string
	events *[]string
	stages []adapter.StartStage
	closes int
	fail   bool
}

func (p *foundationProvider) Type() string { return "test" }
func (p *foundationProvider) Tag() string  { return p.tag }
func (p *foundationProvider) Start(stage adapter.StartStage) error {
	p.stages = append(p.stages, stage)
	if p.events != nil {
		*p.events = append(*p.events, "provider/"+stage.String())
	}
	if p.fail && stage == adapter.StartStateStart {
		return errors.New("provider start witness")
	}
	return nil
}
func (p *foundationProvider) Close() error { p.closes++; return nil }
func (p *foundationProvider) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, errors.New("issuance is not part of this fixture")
}

type foundationInbound struct {
	events *[]string
	stages []adapter.StartStage
	closes int
}

func (i *foundationInbound) Type() string { return "test" }
func (i *foundationInbound) Tag() string  { return "listener" }
func (i *foundationInbound) Start(stage adapter.StartStage) error {
	i.stages = append(i.stages, stage)
	*i.events = append(*i.events, "inbound/"+stage.String())
	return nil
}
func (i *foundationInbound) Close() error { i.closes++; return nil }
