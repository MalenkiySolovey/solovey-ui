package sshbroker

import (
	"strings"
	"testing"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestOpenSSHAdapterRendersConcretePreview(t *testing.T) {
	tries, grace, disabled, enabled := uint16(4), uint32(45), false, true
	prepared, err := prepareOpenSSHPolicy(domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, MaxAuthTries: &tries,
		LoginGraceTimeSeconds: &grace, PasswordAuthentication: &disabled, KbdInteractiveAuthentication: &disabled,
		PermitRootLogin: domain.RootLoginProhibitPassword, PubkeyAuthentication: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Implementation != "openssh" || prepared.Format != "sshd_config_dropin" ||
		prepared.ArtifactDigest != domain.Revision([]byte(prepared.Representation)) {
		t.Fatalf("OpenSSH preview identity = %#v", prepared)
	}
	for _, expected := range []string{"MaxAuthTries 4", "LoginGraceTime 45", "PasswordAuthentication no",
		"KbdInteractiveAuthentication no", "PermitRootLogin prohibit-password", "PubkeyAuthentication yes"} {
		if !strings.Contains(prepared.Representation, expected) {
			t.Fatalf("OpenSSH preview omitted %q: %s", expected, prepared.Representation)
		}
	}
}

func TestDropbearAdapterRendersUCIAndRejectsUnrepresentablePolicy(t *testing.T) {
	tries, disabled := uint16(5), false
	prepared, concrete, err := prepareDropbearUCIPolicy(domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, MaxAuthTries: &tries,
		PasswordAuthentication: &disabled, PermitRootLogin: domain.RootLoginProhibitPassword})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Implementation != "dropbear" || prepared.Format != "uci_change_preview" || concrete.MaxAuthTries == nil ||
		!strings.Contains(prepared.Representation, "dropbear.<selected>.MaxAuthTries") || strings.Contains(prepared.Representation, "sshd_config") {
		t.Fatalf("Dropbear preview = %#v concrete=%#v", prepared, concrete)
	}
	grace := uint32(30)
	if _, _, err := prepareDropbearUCIPolicy(domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1,
		PermitRootLogin: domain.RootLoginUnchanged, LoginGraceTimeSeconds: &grace}); domain.ErrorCode(err) != domain.ReasonUnsupportedDirective {
		t.Fatalf("unrepresentable Dropbear policy error = %v", err)
	}
}
