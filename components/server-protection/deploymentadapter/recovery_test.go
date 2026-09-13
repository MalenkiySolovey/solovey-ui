package deploymentadapter

import (
	"errors"
	"strings"
	"testing"

	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
)

func TestInstalledRecoveryActionsRemainBackendSpecific(t *testing.T) {
	systemd := systemdBrokerRecoveryProjection()
	if systemd.Authority != RecoveryAuthorityAuthenticatedBroker || !systemd.SelfRecoveryAvailable || systemd.Action.Program != "systemctl" || strings.Join(systemd.Action.Args, " ") != "is-active solovey-privileged-broker.socket" {
		t.Fatalf("Systemd recovery action = %#v", systemd)
	}
	procd := procdBrokerRecoveryProjection()
	encoded := procd.Action.Program + " " + strings.Join(procd.Action.Args, " ")
	if procd.Authority != RecoveryAuthorityAuthenticatedBroker || !procd.SelfRecoveryAvailable || procd.Action.Program != "ubus" || !strings.Contains(encoded, `"instance":"root-broker"`) || strings.Contains(encoded, "systemctl") {
		t.Fatalf("procd recovery action = %#v", procd)
	}
}

func TestInstalledRecoveryUsesOnlyRetainedBackend(t *testing.T) {
	for _, test := range []struct {
		backend protectionruntime.DeploymentBackend
		program string
	}{
		{protectionruntime.DeploymentBackendSystemd, "systemctl"},
		{protectionruntime.DeploymentBackendProcd, "ubus"},
		{protectionruntime.DeploymentBackendDocker, "operator_review"},
	} {
		projection, err := recoveryProjectionForBackend(test.backend)
		if err != nil || projection.Action.Program != test.program {
			t.Fatalf("backend %q projection=%#v err=%v", test.backend, projection, err)
		}
	}
	if _, err := recoveryProjectionForBackend("unknown"); err == nil || errors.Is(err, ErrInstalledRecoveryProjectionUnavailable) {
		t.Fatalf("unknown retained backend error = %v", err)
	}
}

func TestDockerRecoveryProjectionIsOperatorManaged(t *testing.T) {
	projection := dockerOperatorRecoveryProjection()
	if projection.Authority != RecoveryAuthorityExternalOperator || projection.SelfRecoveryAvailable || projection.Action.Program != "operator_review" || len(projection.Action.Args) != 1 || projection.Action.Args[0] != "container_replacement_or_restart" || projection.Action.Purpose != "external_operator_managed_recovery" {
		t.Fatalf("Docker recovery projection = %#v", projection)
	}
}
