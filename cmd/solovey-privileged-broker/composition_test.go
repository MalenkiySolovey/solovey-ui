package main

import (
	"reflect"
	"testing"

	deploymentbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	updatebroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func systemdBrokerArgs() []string {
	return []string{"--transport=systemd-activated", "--ssh-implementation=openssh", "--ssh-service-control=systemd", "--ssh-log-evidence=journald", "--deployment-backend=systemd-native", "--update-mode=native-self-managed"}
}

func openWrtBrokerArgs() []string {
	return []string{"--transport=standalone-owned", "--ssh-implementation=dropbear", "--ssh-service-control=procd", "--ssh-log-evidence=logread", "--deployment-backend=package-managed", "--update-mode=package-managed"}
}

func TestDeploymentInjectedOwnerSelectionsResolveExactAdapters(t *testing.T) {
	for name, test := range map[string]struct {
		args       []string
		transport  broker.TransportMode
		ssh        sshbroker.Composition
		deployment deploymentbroker.Backend
		update     updatebroker.Mode
	}{
		"systemd native":          {systemdBrokerArgs(), broker.SystemdActivated, sshbroker.SystemdOpenSSHComposition(), deploymentbroker.BackendSystemdNative, updatebroker.ModeNativeSelfManaged},
		"OpenWrt package managed": {openWrtBrokerArgs(), broker.StandaloneOwned, sshbroker.ProcdDropbearComposition(), deploymentbroker.BackendPackageManaged, updatebroker.ModePackageManaged},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseRuntimeComposition(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if got.transport != test.transport || got.ssh.Composition() != test.ssh ||
				got.deploymentBackend != test.deployment || got.updateMode != test.update {
				t.Fatalf("resolved composition = %#v", got)
			}
		})
	}
}

func TestOwnerCompositionSelectionFailsClosed(t *testing.T) {
	tests := map[string][]string{
		"missing deployment backend":   append(openWrtBrokerArgs()[:4], openWrtBrokerArgs()[5]),
		"missing update mode":          openWrtBrokerArgs()[:5],
		"unknown deployment backend":   append(openWrtBrokerArgs()[:4], "--deployment-backend=unknown", openWrtBrokerArgs()[5]),
		"unknown update mode":          append(openWrtBrokerArgs()[:5], "--update-mode=unknown"),
		"duplicate deployment backend": append(openWrtBrokerArgs(), "--deployment-backend=package-managed"),
		"incomplete SSH composition":   append(openWrtBrokerArgs()[:3], openWrtBrokerArgs()[4:]...),
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRuntimeComposition(args); err == nil {
				t.Fatal("unknown or incomplete broker composition was accepted")
			}
		})
	}
}

func TestBrokerCompositionFieldsBelongToIndependentSemanticOwners(t *testing.T) {
	typeOf := reflect.TypeOf(runtimeComposition{})
	want := map[string]string{
		"transport":         "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker",
		"ssh":               "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker",
		"deploymentBackend": "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker",
		"updateMode":        "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker",
	}
	if typeOf.NumField() != len(want) {
		t.Fatalf("runtime composition fields=%d, want one field per semantic owner=%d", typeOf.NumField(), len(want))
	}
	for name, owner := range want {
		field, ok := typeOf.FieldByName(name)
		if !ok {
			t.Fatalf("runtime composition lacks semantic owner field %q", name)
		}
		if field.Type.PkgPath() != owner {
			t.Fatalf("runtime composition field %q owner=%q, want %q", name, field.Type.PkgPath(), owner)
		}
	}
}
