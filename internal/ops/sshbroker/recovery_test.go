package sshbroker

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func recoveryJournalRow(message, unit, identifier, executable, timestamp, cursor string) []byte {
	value, _ := json.Marshal(map[string]string{"MESSAGE": message, "_SYSTEMD_UNIT": unit, "SYSLOG_IDENTIFIER": identifier, "_EXE": executable, "__REALTIME_TIMESTAMP": timestamp, "__CURSOR": cursor})
	return value
}

func TestOpenSSHRecoveryAcceptsOnlyFreshOwnerBoundPublicKeyFacts(t *testing.T) {
	now := time.Unix(30_000, 0).UTC()
	request := RecoveryObserveRequest{SinceUnixMicros: now.Add(-time.Second).UnixMicro(), MaxEvents: 8}
	timestamp := strconv.FormatInt(now.UnixMicro(), 10)
	rows := [][]byte{
		recoveryJournalRow("Accepted publickey for admin from 198.51.100.10 port 54321 ssh2: key", "ssh.service", "sshd", "/usr/sbin/sshd", timestamp, "cursor-accepted"),
		recoveryJournalRow("Accepted password for admin from 198.51.100.11 port 54322 ssh2", "ssh.service", "sshd", "/usr/sbin/sshd", timestamp, "cursor-password"),
		recoveryJournalRow("session opened for user admin", "ssh.service", "sshd", "/usr/sbin/sshd", timestamp, "cursor-open"),
		recoveryJournalRow("Accepted publickey for admin from 198.51.100.12 port 54323 ssh2", "unrelated-monitor.service", "monitor", "/usr/sbin/sshd", timestamp, "cursor-unrelated"),
		recoveryJournalRow("Accepted publickey for admin from 198.51.100.13 port 54324 ssh2", "ssh.service", "sshd", "/tmp/sshd", timestamp, "cursor-exe"),
	}
	payload := append([]byte(strings.Join([]string{string(rows[0]), string(rows[1]), string(rows[2]), string(rows[3]), string(rows[4])}, "\n")), '\n')
	observations, err := parseOpenSSHRecoveryJournal(payload, request, now, "/usr/sbin/sshd", []string{"ssh.service", "sshd.service"})
	if err != nil || len(observations) != 1 {
		t.Fatalf("accepted observations=%#v err=%v", observations, err)
	}
	observation := observations[0]
	if observation.AuthenticationClass != "publickey" || observation.SourcePrefix != "198.51.100.10/32" ||
		!strings.HasPrefix(observation.PrincipalID, "principal:") || strings.Contains(observation.PrincipalID, "admin") ||
		observation.ObservedAt != now.Unix() || observation.ObservedAtMicros != now.UnixMicro() {
		t.Fatalf("SSH recovery fact leaked or lost identity binding: %#v", observation)
	}
}

func TestOpenSSHRecoveryCanonicalizesAddressAndFailsClosedOnMalformedOrUnboundedEvidence(t *testing.T) {
	now := time.Unix(40_000, 0).UTC()
	request := RecoveryObserveRequest{SinceUnixMicros: now.Add(-time.Second).UnixMicro(), MaxEvents: 1}
	row := recoveryJournalRow("Accepted publickey for admin from ::ffff:192.0.2.10 port 2222 ssh2", "sshd.service", "sshd", "/usr/sbin/sshd", strconv.FormatInt(now.UnixMicro(), 10), "cursor-mapped")
	observations, err := parseOpenSSHRecoveryJournal(append(row, '\n'), request, now, "/usr/sbin/sshd", []string{"ssh.service", "sshd.service"})
	if err != nil || len(observations) != 1 || observations[0].SourcePrefix != "192.0.2.10/32" {
		t.Fatalf("mapped address result=%#v err=%v", observations, err)
	}
	if _, err := parseOpenSSHRecoveryJournal([]byte("not-json\n"), request, now, "/usr/sbin/sshd", []string{"ssh.service"}); err == nil {
		t.Fatal("malformed journal JSON was accepted")
	}
	if _, err := parseOpenSSHRecoveryJournal(bytesOf('x', maxRecoveryEvidenceBytes+1), request, now, "/usr/sbin/sshd", []string{"ssh.service"}); err == nil {
		t.Fatal("unbounded journal output was accepted")
	}
}

func TestRecoveryVerifierRequiresLocalPublicKeysAndStrictOwnership(t *testing.T) {
	complete := "pubkeyauthentication yes\nauthorizedkeyscommand none\nstrictmodes yes\n"
	if !recoveryPublicKeyVerifierConfigurationProven(complete) {
		t.Fatal("complete fail-closed SSH verifier configuration was rejected")
	}
	for _, missing := range []string{"pubkeyauthentication yes\n", "authorizedkeyscommand none\n", "strictmodes yes\n"} {
		if recoveryPublicKeyVerifierConfigurationProven(strings.ReplaceAll(complete, missing, "")) {
			t.Fatalf("SSH verifier accepted configuration missing %q", strings.TrimSpace(missing))
		}
	}
}

func TestDropbearRecoveryIsFreshBoundedAndProcessCorrelated(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	line := "Wed Jan  1 00:00:00 2027 [1800000000.123] daemon.notice dropbear[321]: Pubkey auth succeeded for 'alice' with ssh-ed25519 key SHA256:redacted from 192.0.2.4:50123\n"
	request := RecoveryObserveRequest{SinceUnixMicros: now.Add(-time.Minute).UnixMicro(), MaxEvents: 8}
	result, err := parseDropbearRecoveryLog([]byte(line), request, now, func(pid int) bool { return pid == 321 })
	if err != nil || len(result) != 1 || result[0].AuthenticationClass != "publickey" || result[0].SourcePrefix != "192.0.2.4/32" || strings.Contains(result[0].ObservationID, "alice") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, mutation := range []struct {
		data    string
		matches func(int) bool
	}{
		{strings.Replace(line, "1800000000.123", "1799999000.123", 1), func(int) bool { return true }},
		{strings.Replace(line, "dropbear[321]", "other[321]", 1), func(int) bool { return true }},
		{line, func(int) bool { return false }},
	} {
		result, err := parseDropbearRecoveryLog([]byte(mutation.data), request, now, mutation.matches)
		if err != nil || len(result) != 0 {
			t.Fatalf("uncorrelated result=%#v err=%v", result, err)
		}
	}
	if _, err := parseDropbearRecoveryLog([]byte(strings.Repeat(line, maxRecoveryLogRecords+1)), request, now, func(int) bool { return true }); err == nil {
		t.Fatal("unbounded Dropbear record set was accepted")
	}
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}
