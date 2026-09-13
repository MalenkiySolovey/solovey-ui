package sshbroker

import (
	"reflect"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestRuntimeCompositionDoesNotDeriveSSHImplementationFromTransport(t *testing.T) {
	tests := []struct {
		args           []string
		transport      broker.TransportMode
		implementation Implementation
	}{
		{[]string{"--transport=systemd-activated", "--ssh-implementation=dropbear", "--ssh-service-control=procd", "--ssh-log-evidence=logread"}, broker.SystemdActivated, ImplementationDropbear},
		{[]string{"--transport=standalone-owned", "--ssh-implementation=openssh", "--ssh-service-control=systemd", "--ssh-log-evidence=journald"}, broker.StandaloneOwned, ImplementationOpenSSH},
	}
	for _, test := range tests {
		transport, composition, err := ParseRuntimeArgs(test.args)
		if err != nil || transport != test.transport || composition.Implementation() != test.implementation {
			t.Fatalf("ParseRuntimeArgs(%v) = %q, %#v, %v", test.args, transport, composition, err)
		}
	}
}

func TestCurrentSSHCompositionProfilesRemainExplicit(t *testing.T) {
	for _, composition := range []Composition{SystemdOpenSSHComposition(), ProcdDropbearComposition()} {
		if err := composition.Validate(); err != nil {
			t.Fatalf("current composition %#v rejected: %v", composition, err)
		}
	}
	future := Composition{Implementation: ImplementationOpenSSH, ServiceControl: ServiceControlProcd, LogEvidence: LogEvidenceLogread}
	if err := future.Validate(); err != nil {
		t.Fatalf("independent known capability dimensions were rejected as a semantic impossibility: %v", err)
	}
	if err := future.validateRegistered(); err == nil {
		t.Fatal("unregistered OpenSSH/procd/logread adapters were advertised as shipped")
	}
}

func TestThirdCompositionDoesNotChangeDesiredPolicy(t *testing.T) {
	typeOfPolicy := reflect.TypeOf(domain.DesiredPolicyV1{})
	for index := 0; index < typeOfPolicy.NumField(); index++ {
		field := typeOfPolicy.Field(index)
		forbidden := map[string]bool{"Implementation": true, "Daemon": true, "Service": true, "Supervisor": true, "Transport": true, "LogEvidence": true, "Artifact": true, "DropIn": true, "UCI": true}
		if forbidden[field.Name] {
			t.Fatalf("semantic SSH policy exposes backend-composition field %s", field.Name)
		}
	}
}

func TestServiceControlTargetBindingIsSeparateFromAdapterKind(t *testing.T) {
	future := Composition{Implementation: ImplementationOpenSSH, ServiceControl: ServiceControlProcd, LogEvidence: LogEvidenceLogread}
	catalog := append(releaseSSHCompositionCatalog(), registeredSSHComposition{
		composition: future, serviceTarget: serviceControlTarget{procdService: "sshd"}, logTarget: logEvidenceTarget{},
	})
	registered, err := resolveRegisteredSSHComposition(future, catalog)
	if err != nil {
		t.Fatalf("future composition with an exact trusted target was rejected: %v", err)
	}
	if registered.serviceTarget.procdService != "sshd" || registered.serviceTarget.procdService == "dropbear" {
		t.Fatalf("procd adapter kind selected an implementation target: %#v", registered.serviceTarget)
	}
	if err := future.Validate(); err != nil {
		t.Fatalf("adding a target binding changed capability validity: %v", err)
	}
}

func TestLogEvidenceTargetBindingIsSeparateFromAdapterKind(t *testing.T) {
	future := Composition{Implementation: ImplementationDropbear, ServiceControl: ServiceControlSystemd, LogEvidence: LogEvidenceJournald}
	catalog := append(releaseSSHCompositionCatalog(), registeredSSHComposition{
		composition:   future,
		serviceTarget: serviceControlTarget{systemdUnits: []string{"dropbear.service"}},
		logTarget:     logEvidenceTarget{journaldUnits: []string{"dropbear.service"}},
	})
	registered, err := resolveRegisteredSSHComposition(future, catalog)
	if err != nil {
		t.Fatalf("future journald composition with exact trusted targets was rejected: %v", err)
	}
	if got := registered.logTarget.journaldUnits; len(got) != 1 || got[0] != "dropbear.service" {
		t.Fatalf("journald adapter kind selected OpenSSH units: %#v", got)
	}
}

func TestRegisteredSSHTargetBindingsFailClosed(t *testing.T) {
	composition := ProcdDropbearComposition()
	for name, catalog := range map[string][]registeredSSHComposition{
		"missing target":    {{composition: composition}},
		"cross-kind target": {{composition: composition, serviceTarget: serviceControlTarget{systemdUnits: []string{"dropbear.service"}, procdService: "dropbear"}}},
		"unsafe target":     {{composition: composition, serviceTarget: serviceControlTarget{procdService: "../dropbear"}}},
		"ambiguous binding": {
			{composition: composition, serviceTarget: serviceControlTarget{procdService: "dropbear"}},
			{composition: composition, serviceTarget: serviceControlTarget{procdService: "dropbear-alt"}},
		},
	} {
		if _, err := resolveRegisteredSSHComposition(composition, catalog); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestResolvedSSHCompositionRejectsExactCatalogHybrids(t *testing.T) {
	systemd, err := ResolveRegisteredSSHComposition(SystemdOpenSSHComposition())
	if err != nil {
		t.Fatal(err)
	}
	dropbear, err := ResolveRegisteredSSHComposition(ProcdDropbearComposition())
	if err != nil {
		t.Fatal(err)
	}

	dropbearWithSystemdJournald := systemd
	dropbearWithSystemdJournald.composition.Implementation = ImplementationDropbear

	openSSHWithProcdLogread := dropbear
	openSSHWithProcdLogread.composition = Composition{Implementation: ImplementationOpenSSH, ServiceControl: ServiceControlProcd, LogEvidence: LogEvidenceLogread}

	wrongServiceTarget := systemd
	wrongServiceTarget.serviceTarget.systemdUnits[0] = "attacker.service"

	wrongLogTarget := systemd
	wrongLogTarget.logTarget.journaldUnits[0] = "attacker.service"

	crossRecordTargetMix := systemd
	crossRecordTargetMix.serviceTarget = dropbear.serviceTarget

	for name, test := range map[string]struct {
		resolved ResolvedSSHComposition
		valid    bool
	}{
		"zero value":                       {resolved: ResolvedSSHComposition{}, valid: false},
		"exact systemd openssh journald":   {resolved: systemd, valid: true},
		"exact procd dropbear logread":     {resolved: dropbear, valid: true},
		"dropbear systemd journald hybrid": {resolved: dropbearWithSystemdJournald, valid: false},
		"openssh procd logread hybrid":     {resolved: openSSHWithProcdLogread, valid: false},
		"wrong service target":             {resolved: wrongServiceTarget, valid: false},
		"wrong log target":                 {resolved: wrongLogTarget, valid: false},
		"cross-record target mix":          {resolved: crossRecordTargetMix, valid: false},
	} {
		if got := test.resolved.Valid(); got != test.valid {
			t.Errorf("%s: Valid()=%v, want %v", name, got, test.valid)
		}
	}
}

func TestResolvedSSHCompositionProjectionsAreDefensive(t *testing.T) {
	resolved, err := ResolveRegisteredSSHComposition(SystemdOpenSSHComposition())
	if err != nil {
		t.Fatal(err)
	}
	composition := resolved.Composition()
	composition.Implementation = ImplementationDropbear
	units := resolved.logTarget.journaldUnitList()
	units[0] = "attacker.service"
	units = append(units, "another-attacker.service")
	copied := resolved
	copied.serviceTarget.systemdUnits[0] = "attacker.service"
	copied.logTarget.journaldUnits[0] = "attacker.service"

	if !resolved.Valid() || resolved.Implementation() != ImplementationOpenSSH ||
		resolved.ServiceControl() != ServiceControlSystemd || resolved.LogEvidence() != LogEvidenceJournald ||
		!sameStringSlices(resolved.logTarget.journaldUnitList(), []string{"ssh.service", "sshd.service"}) {
		t.Fatalf("resolved composition was changed through a projection: %#v", resolved)
	}
}
