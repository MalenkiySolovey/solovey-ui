package privilegedbroker

// A released proof-client manifest has no pinned unit because the OpenSSH unit
// name varies between ssh.service and sshd.service. It still requires an
// observed, syntactically valid systemd cgroup. Panel entries pin their exact
// unit and must continue to match it.
func systemdCgroupProofMatches(expected, observed string) bool {
	return observed != "" && (expected == "" || expected == observed)
}
