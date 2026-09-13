package sshbroker

import (
	"fmt"
	"strings"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type dropbearUCIPolicy struct {
	MaxAuthTries     *uint16
	PasswordAuth     *bool
	RootLogin        *bool
	RootPasswordAuth *bool
}

func prepareOpenSSHPolicy(policy domain.DesiredPolicyV1) (PreparedPolicyV1, error) {
	if err := policy.Validate(); err != nil {
		return PreparedPolicyV1{}, err
	}
	lines := []string{"# Managed by Solovey UI; typed policy only."}
	if policy.MaxAuthTries != nil {
		lines = append(lines, fmt.Sprintf("MaxAuthTries %d", *policy.MaxAuthTries))
	}
	if policy.LoginGraceTimeSeconds != nil {
		lines = append(lines, fmt.Sprintf("LoginGraceTime %d", *policy.LoginGraceTimeSeconds))
	}
	if policy.PasswordAuthentication != nil {
		lines = append(lines, "PasswordAuthentication "+yesNoPolicy(*policy.PasswordAuthentication))
	}
	if policy.KbdInteractiveAuthentication != nil {
		lines = append(lines, "KbdInteractiveAuthentication "+yesNoPolicy(*policy.KbdInteractiveAuthentication))
	}
	switch policy.PermitRootLogin {
	case domain.RootLoginYes:
		lines = append(lines, "PermitRootLogin yes")
	case domain.RootLoginNo:
		lines = append(lines, "PermitRootLogin no")
	case domain.RootLoginProhibitPassword:
		lines = append(lines, "PermitRootLogin prohibit-password")
	}
	if policy.PubkeyAuthentication != nil {
		lines = append(lines, "PubkeyAuthentication "+yesNoPolicy(*policy.PubkeyAuthentication))
	}
	representation := strings.Join(lines, "\n") + "\n"
	return PreparedPolicyV1{Implementation: "openssh", Format: "sshd_config_dropin", Label: "OpenSSH managed drop-in",
		Representation: representation, ArtifactDigest: domain.Revision([]byte(representation))}, nil
}

func prepareDropbearUCIPolicy(policy domain.DesiredPolicyV1) (PreparedPolicyV1, dropbearUCIPolicy, error) {
	if err := policy.Validate(); err != nil {
		return PreparedPolicyV1{}, dropbearUCIPolicy{}, err
	}
	if policy.LoginGraceTimeSeconds != nil || policy.PubkeyAuthentication != nil && !*policy.PubkeyAuthentication ||
		policy.KbdInteractiveAuthentication != nil && *policy.KbdInteractiveAuthentication ||
		policy.PasswordAuthentication != nil && *policy.PasswordAuthentication && policy.KbdInteractiveAuthentication != nil && !*policy.KbdInteractiveAuthentication {
		return PreparedPolicyV1{}, dropbearUCIPolicy{}, domain.NewError("dropbear_policy", domain.ReasonUnsupportedDirective)
	}
	concrete := dropbearUCIPolicy{MaxAuthTries: policy.MaxAuthTries, PasswordAuth: policy.PasswordAuthentication}
	if policy.KbdInteractiveAuthentication != nil {
		value := false
		concrete.PasswordAuth = &value
	}
	switch policy.PermitRootLogin {
	case domain.RootLoginUnchanged:
	case domain.RootLoginYes:
		yes := true
		concrete.RootLogin, concrete.RootPasswordAuth = &yes, &yes
	case domain.RootLoginNo:
		no := false
		concrete.RootLogin = &no
	case domain.RootLoginProhibitPassword:
		yes, no := true, false
		concrete.RootLogin, concrete.RootPasswordAuth = &yes, &no
	default:
		return PreparedPolicyV1{}, dropbearUCIPolicy{}, domain.NewError("dropbear_policy", domain.ReasonUnsupportedDirective)
	}
	lines := []string{"# Dropbear UCI change preview; target section is selected by EndpointID."}
	if concrete.MaxAuthTries != nil {
		lines = append(lines, fmt.Sprintf("set dropbear.<selected>.MaxAuthTries='%d'", *concrete.MaxAuthTries))
	}
	if concrete.PasswordAuth != nil {
		lines = append(lines, "set dropbear.<selected>.PasswordAuth='"+bool01(*concrete.PasswordAuth)+"'")
	}
	if concrete.RootLogin != nil {
		lines = append(lines, "set dropbear.<selected>.RootLogin='"+bool01(*concrete.RootLogin)+"'")
	}
	if concrete.RootPasswordAuth != nil {
		lines = append(lines, "set dropbear.<selected>.RootPasswordAuth='"+bool01(*concrete.RootPasswordAuth)+"'")
	}
	if policy.PubkeyAuthentication != nil {
		lines = append(lines, "# assert Dropbear public-key authentication remains enabled")
	}
	lines = append(lines, "set dropbear.<selected>.SoloveyPolicyDigest='<candidate-digest>'")
	representation := strings.Join(lines, "\n") + "\n"
	prepared := PreparedPolicyV1{Implementation: "dropbear", Format: "uci_change_preview", Label: "Dropbear UCI selected-section changes",
		Representation: representation, ArtifactDigest: domain.Revision([]byte(representation))}
	return prepared, concrete, nil
}

func yesNoPolicy(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func bool01(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
