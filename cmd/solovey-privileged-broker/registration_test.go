package main

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	deploymentbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	updatebroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

type registrationFixture struct {
	facilities map[string]bool
	order      []string
	deployment deploymentbroker.Backend
	update     updatebroker.Mode
}

func (fixture *registrationFixture) require(owner, capability, adapter string, facilities ...string) error {
	for _, facility := range facilities {
		if !fixture.facilities[facility] {
			return broker.StartupFailure(owner, capability, adapter, "required_executable_unavailable", errors.New("fixture executable is absent"))
		}
	}
	return nil
}

func (fixture *registrationFixture) registrars() handlerRegistrars {
	register := func(registry *broker.Registry, verb broker.Verb) error {
		return registry.Register(verb, broker.Definition{Role: broker.RolePanel, Handler: func(context.Context, broker.Request, broker.PeerIdentity) (any, error) { return nil, nil }})
	}
	return handlerRegistrars{
		contributed: func(registry *broker.Registry) error {
			fixture.order = append(fixture.order, "contributed")
			return register(registry, "test.contributed")
		},
		serverProtection: func(registry *broker.Registry, composition sshbroker.ResolvedSSHComposition) error {
			fixture.order = append(fixture.order, "server-protection")
			if !composition.Valid() {
				return errors.New("invalid SSH projection")
			}
			if err := fixture.require("server-protection", "firewall.manage", "nft", "nft"); err != nil {
				return err
			}
			return register(registry, "test.server-protection")
		},
		ssh: func(registry *broker.Registry, composition sshbroker.ResolvedSSHComposition, _ broker.CompletedMutationAuthority) error {
			fixture.order = append(fixture.order, "ssh")
			switch composition.Composition() {
			case sshbroker.ProcdDropbearComposition():
				if err := fixture.require("ssh", "service.supervision", "procd", "procd", "ubus"); err != nil {
					return err
				}
				if err := fixture.require("ssh", "ssh.security_log_evidence", "logread", "logread"); err != nil {
					return err
				}
				if err := fixture.require("ssh", "ssh.management", "dropbear", "uci", "dropbear"); err != nil {
					return err
				}
			case sshbroker.SystemdOpenSSHComposition():
				if err := fixture.require("ssh", "service.supervision", "systemd", "systemctl"); err != nil {
					return err
				}
				if err := fixture.require("ssh", "ssh.security_log_evidence", "journald", "journalctl"); err != nil {
					return err
				}
				if err := fixture.require("ssh", "ssh.management", "openssh", "sshd"); err != nil {
					return err
				}
			default:
				return errors.New("fixture SSH composition is unknown")
			}
			return register(registry, "test.ssh")
		},
		deployment: func(registry *broker.Registry, backend deploymentbroker.Backend, authority broker.CompletedMutationAuthority) error {
			_ = authority
			fixture.order = append(fixture.order, "deployment")
			fixture.deployment = backend
			if backend == deploymentbroker.BackendSystemdNative {
				if err := fixture.require("deployment", "service.supervision", "systemd", "systemctl"); err != nil {
					return err
				}
				return register(registry, "test.deployment")
			}
			if backend != deploymentbroker.BackendPackageManaged {
				return errors.New("fixture deployment backend is unknown")
			}
			return nil
		},
		update: func(registry *broker.Registry, mode updatebroker.Mode) error {
			fixture.order = append(fixture.order, "update")
			fixture.update = mode
			if mode == updatebroker.ModeNativeSelfManaged {
				return register(registry, "test.update")
			}
			if mode != updatebroker.ModePackageManaged {
				return errors.New("fixture update mode is unknown")
			}
			return nil
		},
	}
}

type startupJournal struct{}

func (startupJournal) Begin(broker.Request, broker.PeerIdentity, string, time.Time) (*broker.Response, *broker.Receipt, error) {
	return nil, nil, errors.New("not used")
}
func (startupJournal) Commit(_ broker.Request, _ *broker.Receipt, response broker.Response, _ broker.CompletionPolicy, _ time.Time) (broker.Response, error) {
	return response, errors.New("not used")
}
func (startupJournal) Unresolved() []broker.Receipt { return nil }

type startupAttestor struct{}

func (startupAttestor) Attest(context.Context, *net.UnixConn, broker.Role) (broker.PeerIdentity, error) {
	return broker.PeerIdentity{Revision: broker.Digest([]byte("peer"))}, nil
}
func (startupAttestor) Recheck(context.Context, broker.PeerIdentity, broker.Role) error { return nil }

func assertServerConstruction(t *testing.T, registry *broker.Registry) {
	t.Helper()
	if _, err := broker.NewServer(registry, startupJournal{}, startupAttestor{}, "boot"); err != nil {
		t.Fatalf("server construction failed after registration: %v", err)
	}
}

func TestOpenWrtFullBrokerRegistrationUsesOnlyOpenWrtFacilities(t *testing.T) {
	composition, err := parseRuntimeComposition(openWrtBrokerArgs())
	if err != nil {
		t.Fatal(err)
	}
	fixture := &registrationFixture{facilities: map[string]bool{
		"procd": true, "ubus": true, "uci": true, "dropbear": true, "logread": true,
		"nft": true, "fw4": true, "apk": true,
		// systemctl, systemd-analyze, journalctl, and sshd are intentionally absent.
	}}
	registry := broker.NewRegistry()
	if err := registerHandlerGraph(registry, composition, nil, fixture.registrars()); err != nil {
		t.Fatalf("OpenWrt full broker registration failed: %v", err)
	}
	if want := []string{"contributed", "server-protection", "ssh", "deployment", "update"}; !reflect.DeepEqual(fixture.order, want) {
		t.Fatalf("registration order = %v, want %v", fixture.order, want)
	}
	if fixture.deployment != deploymentbroker.BackendPackageManaged || fixture.update != updatebroker.ModePackageManaged {
		t.Fatalf("OpenWrt selected native authority: deployment=%q update=%q", fixture.deployment, fixture.update)
	}
	assertServerConstruction(t, registry)
}

func TestOpenWrtRegistrationFailsClosedForMissingSelectedCapability(t *testing.T) {
	composition, err := parseRuntimeComposition(openWrtBrokerArgs())
	if err != nil {
		t.Fatal(err)
	}
	fixture := &registrationFixture{facilities: map[string]bool{"procd": true, "uci": true, "dropbear": true, "logread": true, "nft": true, "fw4": true, "apk": true}}
	err = registerHandlerGraph(broker.NewRegistry(), composition, nil, fixture.registrars())
	if err == nil || err.Error() != "owner=ssh capability=service.supervision adapter=procd reason=required_executable_unavailable" {
		t.Fatalf("missing OpenWrt ubus diagnostic = %v", err)
	}
}

func TestNativeSystemdFullBrokerRegistrationAndMissingSystemctl(t *testing.T) {
	composition, err := parseRuntimeComposition(systemdBrokerArgs())
	if err != nil {
		t.Fatal(err)
	}
	complete := &registrationFixture{facilities: map[string]bool{"nft": true, "systemctl": true, "systemd-analyze": true, "journalctl": true, "sshd": true}}
	registry := broker.NewRegistry()
	if err := registerHandlerGraph(registry, composition, nil, complete.registrars()); err != nil {
		t.Fatalf("native full broker registration failed: %v", err)
	}
	if complete.deployment != deploymentbroker.BackendSystemdNative || complete.update != updatebroker.ModeNativeSelfManaged {
		t.Fatalf("native composition selected wrong authority: deployment=%q update=%q", complete.deployment, complete.update)
	}
	assertServerConstruction(t, registry)

	missing := &registrationFixture{facilities: map[string]bool{"nft": true, "journalctl": true, "sshd": true}}
	err = registerHandlerGraph(broker.NewRegistry(), composition, nil, missing.registrars())
	if err == nil || err.Error() != "owner=ssh capability=service.supervision adapter=systemd reason=required_executable_unavailable" {
		t.Fatalf("missing native systemctl diagnostic = %v", err)
	}
}
