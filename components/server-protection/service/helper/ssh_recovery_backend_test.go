package helper

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sshJournalRow(message, unit, identifier, executable, timestamp, cursor string) []byte {
	value, _ := json.Marshal(map[string]string{"MESSAGE": message, "_SYSTEMD_UNIT": unit, "SYSLOG_IDENTIFIER": identifier, "_EXE": executable, "__REALTIME_TIMESTAMP": timestamp, "__CURSOR": cursor})
	return value
}

func TestSSHRecoveryParserAcceptsOnlyFreshStructuredPublicKeyAuthentication(t *testing.T) {
	now := time.Unix(30_000, 0).UTC()
	request := SSHRecoveryObserveRequest{SinceUnixMicros: now.Add(-time.Second).UnixMicro(), MaxEvents: 8}
	timestamp := strconv.FormatInt(now.UnixMicro(), 10)
	rows := [][]byte{
		sshJournalRow("Accepted publickey for admin from 198.51.100.10 port 54321 ssh2: key", "ssh.service", "sshd", "/usr/sbin/sshd", timestamp, "cursor-accepted"),
		sshJournalRow("Accepted password for admin from 198.51.100.11 port 54322 ssh2", "ssh.service", "sshd", "/usr/sbin/sshd", timestamp, "cursor-password"),
		sshJournalRow("session opened for user admin", "ssh.service", "sshd", "/usr/sbin/sshd", timestamp, "cursor-open"),
		sshJournalRow("Accepted publickey for admin from 198.51.100.12 port 54323 ssh2", "unrelated-monitor.service", "monitor", "/usr/sbin/sshd", timestamp, "cursor-unrelated"),
		sshJournalRow("Accepted publickey for admin from 198.51.100.13 port 54324 ssh2", "ssh.service", "sshd", "/tmp/sshd", timestamp, "cursor-exe"),
	}
	payload := append([]byte(strings.Join([]string{string(rows[0]), string(rows[1]), string(rows[2]), string(rows[3]), string(rows[4])}, "\n")), '\n')
	observations, err := parseSSHRecoveryJournal(payload, request, now, "/usr/sbin/sshd")
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("accepted observation count=%d", len(observations))
	}
	observation := observations[0]
	if observation.AuthenticationClass != "publickey" || observation.SourcePrefix != "198.51.100.10/32" || !strings.HasPrefix(observation.PrincipalID, "principal:") || strings.Contains(observation.PrincipalID, "admin") || observation.ObservedAt != now.Unix() || observation.ObservedAtMicros != now.UnixMicro() {
		t.Fatalf("SSH observation leaked or lost identity binding: %#v", observation)
	}
}

func TestSSHRecoveryParserCanonicalizesMappedAddressAndRejectsMalformedJournal(t *testing.T) {
	now := time.Unix(40_000, 0).UTC()
	request := SSHRecoveryObserveRequest{SinceUnixMicros: now.Add(-time.Second).UnixMicro(), MaxEvents: 1}
	row := sshJournalRow("Accepted publickey for admin from ::ffff:192.0.2.10 port 2222 ssh2", "sshd.service", "sshd", "/usr/sbin/sshd", strconv.FormatInt(now.UnixMicro(), 10), "cursor-mapped")
	observations, err := parseSSHRecoveryJournal(append(row, '\n'), request, now, "/usr/sbin/sshd")
	if err != nil || len(observations) != 1 || observations[0].SourcePrefix != "192.0.2.10/32" {
		t.Fatalf("mapped SSH address was not canonicalized: observations=%#v err=%v", observations, err)
	}
	if _, err := parseSSHRecoveryJournal([]byte("not-json\n"), request, now, "/usr/sbin/sshd"); err == nil {
		t.Fatal("malformed journal JSON was accepted")
	}
}

func TestSSHRecoveryVerifierRequiresLocalKeysAndStrictOwnershipChecks(t *testing.T) {
	complete := "pubkeyauthentication yes\nauthorizedkeyscommand none\nstrictmodes yes\n"
	if !sshPublicKeyVerifierConfigurationProven(complete) {
		t.Fatal("complete fail-closed SSH verifier configuration was rejected")
	}
	for _, missing := range []string{"pubkeyauthentication yes\n", "authorizedkeyscommand none\n", "strictmodes yes\n"} {
		if sshPublicKeyVerifierConfigurationProven(strings.ReplaceAll(complete, missing, "")) {
			t.Fatalf("SSH verifier accepted configuration missing %q", strings.TrimSpace(missing))
		}
	}
}
